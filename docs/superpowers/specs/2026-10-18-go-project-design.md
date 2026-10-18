# go-project template + gokit v0.4.0 — Design

Date: 2026-10-18
Status: approved 2026-10-18

## Intent

The user wants a personal Go multi-service boilerplate, `go-project`, derived from the
structure of AssetPulse (`~/GitHub/assetpulse/assetpulse-app`) and built on their shared
library gokit (`~/GitHub/gokit`, module `github.com/piresc/gokit`).

`~/GitHub/assessment/boilerplate-golang` is the company assessment and stays exactly as it is.
Nothing in this work touches it. It pins gokit v0.3.0, so a new gokit release does not affect it.

This is two projects, done in order:

1. **gokit v0.4.0**: add RS256 token signing/verification and token revocation checks, so
   the shared auth middleware AssetPulse keeps in `services/pkg/auth` lives in gokit instead.
2. **go-project**: new repo. AssetPulse's folder structure. `user-service` is a real,
   working port of AssetPulse `uss`. The other services are skeletons only.

## Goals

- Starting a new project = copy `go-project`, rename, fill in skeleton services.
- Every service starts through gokit `bootstrap`; no hand-written DB/Redis/NATS/shutdown code.
- user-service behaves like AssetPulse uss: same endpoints, same token model, same events.
- Auth for every service comes from gokit (RS256 + revocation). No `services/pkg/auth`.

## Non-goals

- No changes to `boilerplate-golang` or anything under `~/GitHub/assessment`.
- No changes to AssetPulse.
- No frontend (React app) in go-project (builder call; user did not ask for it).
- No business logic in skeleton services.
- No Google Wire (decision below).

## Anti-goals

- Copying secrets: AssetPulse `keys/`, `.env`, real Coolify network IDs / domains must not appear in go-project.
- Breaking existing gokit consumers: the HS256 `auth.GenerateToken/ValidateToken` and
  `middleware.JWTAuth` APIs stay unchanged in v0.4.0.
- A template that doesn't build: every skeleton package must compile and `go vet` clean.

---

## Part 1 — gokit v0.4.0

Repo: `~/GitHub/gokit`, branch off `master` (currently tag v0.3.0 = 6008f18).

### 1a. Fix existing breakage first (builder call)

v0.3.0 has a test that doesn't compile and unformatted files. A release on top of that is not
clean, so fix first:
- `bootstrap/bootstrap_test.go:122` uses removed `app.Engine` → assert on `app.Mux`.
- `gofmt -w` on: `tracing/noop.go`, `logger/logger_test.go`, `health/health_test.go`,
  `testkit/cleanup.go`, `testkit/containers.go`, `ratelimit/store_test.go`.
- README: add missing `geo`, `semver`, `httpclient` rows; drop the "app-template replace" wording.

### 1b. RS256 in `auth`

Additive, in package `auth` (new file(s), e.g. `auth/rsa.go`, `auth/keys.go`):

- Key loading, ported from uss `internal/app/providers.go`:
  - `LoadRSAPrivateKey(pemValue, path string) (*rsa.PrivateKey, error)` — PEM string wins over
    file path; accepts literal `\n` escapes (env vars); PKCS#1 and PKCS#8.
  - `LoadRSAPublicKey(pemValue, path string) (*rsa.PublicKey, error)` — PKIX.
- Claims type for RS256 tokens, matching uss's token shape: `sub` = user ID, `email`, `role`,
  `jti`, `exp`, `iat`, `iss`. Exact Go type name/layout is the builder's call; it must round-trip
  tokens issued by uss's `gateway/token/jwt.go` format.
- Signer: generate an access token for (userID, email, role) with issuer + TTL, returning
  token, jti, expiry (same contract as uss `TokenService.GenerateAccessToken`).
- Verifier: parse + verify with public key; reject non-RSA `alg`; distinguish expired from invalid
  (sentinel errors, e.g. `ErrTokenExpired`, `ErrTokenInvalid`).
- Refresh-token generation (random opaque token + SHA-256 hash + expiry) stays in user-service,
  not gokit — it's not shared by other services (builder call).

### 1c. Revocation

- `auth.RevocationChecker` interface: `IsRevoked(ctx, jti string) (bool, error)`.
- Redis implementation using gokit's `redis.Client`, key format `revoked:<jti>` (same as AssetPulse,
  so the same Redis data works). Also a `Revoke(ctx, jti, ttl)` writer used by user-service on logout.
  Package placement (in `auth` vs a sub-package to keep `auth` free of Redis imports) is the builder's call.
- **Redis error behavior: fail-closed by default** (user decision: "what gokit uses"). gokit never
  swallows Redis errors: `redislock` and `idempotency` return them to the caller. So the revocation
  checker returns the error, and the middleware rejects the request with **503 Service Unavailable
  + `Retry-After: 5`** (user decision; not 401/403, because the token isn't known to be bad and
  clients must retry rather than refresh, log out, or show "access denied"). An explicit option enables fail-open (AssetPulse's
  behavior) for services that prefer availability; the failure is logged either way.
  The revocation lookup has its own short timeout (default 1s) so a hung Redis returns 503 quickly
  instead of hitting nginx's 5s proxy timeout.

### 1d. Middleware

New method on gokit `middleware.Middleware`, alongside the untouched `JWTAuth`:

- `RS256Auth(verifier, revocation /*nil = skip*/) func(http.Handler) http.Handler` — required auth.
- Optional-auth variant (no header → pass through; bad header → 401), as in AssetPulse
  `OptionalAuthenticate`.
- Puts the full claims (user ID, email, role, jti, exp) in the request context with an accessor
  (e.g. `middleware.GetClaims(r)`); `GetUserID`/`GetRole` keep working for RS256 too.
- `RequireRole` works unchanged after either middleware.
- Error bodies use gokit's existing envelope (user decision): `response.Error` →
  `{"status":"error","error":"<message>"}`. Messages distinguish the cases: missing/invalid token,
  token expired, token revoked. No AssetPulse-style `code` field.

### 1e. Config

- Add to `config.Base` (or a new `AuthConfig` block — builder's call) the RS256 settings:
  private key PEM / path, public key PEM / path, issuer, access TTL, refresh TTL.
  Env bindings: `JWT_PRIVATE_KEY_PEM`, `JWT_PRIVATE_KEY_PATH`, `JWT_PUBLIC_KEY_PEM`,
  `JWT_PUBLIC_KEY_PATH`, `ACCESS_TOKEN_TTL` (default 15m), `REFRESH_TOKEN_TTL` (default 720h) —
  same names as AssetPulse.
- Existing `JWT_SECRET` validation unchanged; a service using only RS256 must not need `JWT_SECRET`.

### 1f. Release

- Tests: unit tests for key loading (PEM/file/escaped/PKCS1/PKCS8/bad input), sign→verify
  round-trip, wrong alg, expired, revoked, Redis down (fail-open and fail-closed), optional auth.
  Use miniredis (already a dependency).
- `go build`, `go vet`, `go test -race`, `gofmt -l`, `golangci-lint` all clean.
- Tag `v0.4.0` and push branch + tag (user approves the push).

---

## Part 2 — go-project

Location: `~/GitHub/go-project`, its own git repo (builder call; next to `gokit`).
Module layout: **one Go module per service**, like AssetPulse (each `services/<name>/go.mod`),
plus a `go.work` at the root for local dev. (Builder call, mirrors AssetPulse; with
`services/pkg/auth` gone there's no shared local module.)
Go version: 1.26 (matches gokit's `go 1.26.0`). gokit pinned at `v0.4.0`.

### Tree

```
go-project/
  README.md  AGENTS.md  Makefile  .gitignore  .dockerignore  go.work
  docs/
    ARCHITECTURE.md                    layering rules (adapted from AssetPulse openwiki/architecture.md)
  deploy/
    docker-compose.yml                 local: nginx, user-service, postgres, redis, nats;
                                       asset-service / price-service blocks commented out
    docker-compose.coolify.yml         Coolify deploy (user decision), ported from AssetPulse with the
                                       domain, Traefik network ID and router names replaced by
                                       placeholders/env vars; same service set as docker-compose.yml
    runtime.coolify.env.example        Coolify runtime variables, no real values
                                       (staging compose not ported — builder call)
    nginx.conf                         /api/v1/auth/*, /api/v1/users/* → user-service;
                                       asset/price routes commented out
    Dockerfile.nginx  Dockerfile.postgres
    bootstrap/init-db.sh               creates per-service databases
    .env.example                       every variable, no real values
    e2e_test.sh                        user-service flows only
  services/
    user-service/                      REAL (port of uss)
    asset-service/                     SKELETON (was ams)
    price-service/                     SKELETON (was pss)
```

Names: `uss` → `user-service` (user's decision); `ams` → `asset-service`, `pss` → `price-service`
(builder call for consistency).

### Per-service layout (all three)

```
services/<name>/
  Dockerfile  go.mod  go.sum
  config/local/<name>.yaml             gokit config file (config.Load reads config/<APP_ENV>/<name>.yaml + env)
  migrations/                          embedded SQL (services that own a DB)
  cmd/server/main.go                   thin: app.New() → Run()
  internal/
    app/        app.go: calls bootstrap.NewHTTPService[Config], builds adapters → use cases →
                handlers, registers routes on app.Mux, runs migrations. config.go: service-specific Config T.
    domain/     entities, value objects, validation
    usecase/    business logic; ports.go holds the interfaces the use cases need; errors.go
    repository/postgres/   implements repository ports
    gateway/<thing>/       outbound adapters (cache, messaging, oauth, market, ...)
    handler/http/          transport only: DTOs, routes, error mapping
```

Wiring: by hand in `internal/app`, no Wire (builder call: gokit bootstrap already does the infra
graph, leaving only a few constructor calls).

### user-service (real)

Port of `assetpulse-app/services/uss`, behavior preserved:

| Endpoint | Auth | Behavior |
|---|---|---|
| `POST /auth/google` | none | verify Google ID token, find-or-create user, issue token pair |
| `POST /auth/apple` | none | same for Apple |
| `POST /auth/refresh` | none | rotate refresh token, new pair |
| `POST /auth/logout` | RS256 | revoke current access-token jti + all user refresh tokens |
| `GET /users/me` | RS256 | profile (cache-aside via Redis) |
| `PATCH /users/me` | RS256 | update display name |

Routes are mounted without the `/api/v1` prefix; nginx strips it (as AssetPulse).
Health: `/health/live`, `/health/ready` come from gokit bootstrap automatically.

Mapping from uss to go-project:

| uss | user-service |
|---|---|
| `internal/app/providers.go` (pgxpool, Redis, NATS, PEM loading, migrations) | gokit `bootstrap` + `auth.LoadRSA*` + `bootstrap.RunMigrations` (embedded FS) |
| `internal/app/app.go` lifecycle | `app.Run()` from gokit |
| `internal/app/wire*.go` | removed |
| `pkg/auth` middleware + `handler/http/middleware.go` ClaimsAdapter + `context.go` | gokit `middleware.RS256Auth` + `GetClaims` |
| `gateway/token/jwt.go` | access token via gokit auth signer; refresh-token generation stays local |
| `gateway/cache/redis.go` | same port, on gokit `redis.Client`; revocation writes via gokit revocation store |
| `gateway/messaging/nats.go` (`user.created`) | same port, on gokit `nats.Client`; core NATS publish via `client.Conn().Publish` (not JetStream), as uss does |
| `gateway/oauth/{google,apple}.go` wrapping `internal/oauth` | merged: the real verifiers from `internal/oauth` move into `gateway/oauth` (JWKS verification, cache warm-up), adapter layer removed |
| `repository/postgres` on pgxpool | rewritten on `*sqlx.DB` from bootstrap (same SQL) |
| chi router | `app.Mux` (`net/http`, Go 1.22 method patterns), RS256 routes wrapped per-route |
| `log.Printf` | `app.Logger` (slog) |
| `internal/oauth`, `internal/token`, `internal/models` (real code wrapped by gateway adapters) | folded into `gateway/oauth`, gokit auth + local refresh-token code, and `domain`; no separate packages |
| `bugfix_*`/property tests | port the ones that still apply to kept code; drop the rest |

Schema: copy `deploy/bootstrap/uss/001_create_tables.sql` into `services/user-service/migrations/`
as golang-migrate up/down files.

Config: `Config` T holds `GoogleClientID`, `AppleClientID`; DB/Redis/NATS/auth come from gokit `Base`.
Required at startup: DB, Redis, NATS, private + public key. Missing → fail fast with a clear error.

Tests: unit tests for use cases (fakes for ports), handlers (httptest), repository (gokit testkit
Postgres container), cache/revocation (miniredis), NATS publisher, OAuth verifiers (as in uss).

### asset-service / price-service (skeletons)

- Full folder layout above; each package has one `.go` file with a package doc comment and a
  placeholder (e.g. `type Asset struct{ ID string }`, an empty port interface, a no-op handler that
  registers nothing or a single `GET /ping`).
- `cmd/server/main.go` + `internal/app` call `bootstrap.NewHTTPService` and use
  `middleware.RS256Auth` with the public key only — showing how a non-auth service verifies tokens.
- They compile and `go vet` clean. No tests required beyond what's needed to compile (builder call).
- Not started in `docker-compose.yml` by default (commented blocks).

### Repo hygiene

- `.gitignore`: binaries (`/services/*/server`, `bin/`), `.env`, `*.pem`, coverage files, `.DS_Store`.
- No keys committed. README explains generating an RSA pair with `openssl` for local dev.
- Makefile targets: `test` (all modules), `lint`, `build`, `up`/`down` (compose), `e2e`, `keys` (local dev keypair).

### Done means

- gokit v0.4.0 tagged; all gokit checks clean.
- go-project: every module builds, vets, lints; user-service tests pass with `-race`.
- `docker compose up` starts nginx + user-service + infra; `deploy/e2e_test.sh` passes against it
  (Google/Apple flows exercised with a test seam or skipped with a clear note — they need real provider tokens).

### Settled after builder review (builder calls)

- **Standalone, not interoperable with live AssetPulse.** go-project is a template for new projects:
  its own key pair, databases and Redis. Token issuer is configurable, default `user-service`.
  "Same token model" means the same claim shape and flows, not shared keys or data.
- **Container builds fetch gokit from the public Go proxy** (`github.com/piresc/gokit` is public;
  v0.3.0 resolves on proxy.golang.org, v0.4.0 will once tagged). Each Dockerfile builds from its own
  service directory with `GOWORK=off`; `go.work` is for local dev only. No `replace`, no tokens.
- **Seed account kept**, ported from AssetPulse `deploy/bootstrap/seed-test-account.sql`
  (`testuser@example.com` + fixed refresh token), applied only by an explicit `make seed` after
  user-service has run its migrations (Postgres init can't seed: the tables don't exist yet). The README
  warns never to seed production. `SEED_TEST_ACCOUNT` is dropped. `e2e_test.sh` uses it for the
  refresh / me / logout flows and drops AssetPulse's asset-service steps. Google/Apple sign-in are not
  exercised by e2e (they need real provider tokens); unit tests cover them.

## User decisions

- Auth: RS256 + revocation added to gokit v0.4.0; `services/pkg/auth` removed.
- `uss` renamed `user-service`; only user-service is real, the rest is skeleton folders and stub Go files.
- Every service uses gokit.
- Response envelope: gokit's.
- Revocation on Redis failure: follow gokit (errors surface → fail-closed by default, fail-open opt-in).
- Status codes: no/invalid/wrong-alg token → 401; expired → 401; revoked → 401; Redis unreachable →
  503 + `Retry-After: 5` (fail-open option: allow + warn log); missing role (`RequireRole`) → 403.
- Coolify deployment included.

## Builder calls (listed for review)

Fixing gokit v0.3.0 breakage inside v0.4.0; refresh tokens stay in user-service; ams/pss renamed;
no frontend; repo at `~/GitHub/go-project`; one module per service + go.work; no Wire; skeletons
untested and not in compose by default; OAuth verifiers copied as-is.
