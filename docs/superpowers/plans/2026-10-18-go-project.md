# go-project Template Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `go-project` multi-service template: AssetPulse's structure, a real `user-service` ported from AssetPulse `uss` on gokit v0.4.0, and compile-only skeletons for `asset-service` and `price-service`, plus local and Coolify deployment.

**Architecture:** One Go module per service under `services/`, a root `go.work` for local dev. Each service starts via `bootstrap.NewHTTPService[Config]`, wires its adapters → use cases → handlers by hand in `internal/app`, and registers routes on `app.Mux`. Auth is RS256 from gokit: user-service signs, every service verifies with the public key and checks revocation in Redis. nginx is the single entry point and strips `/api/v1`.

**Tech Stack:** Go 1.26, `github.com/piresc/gokit v0.4.0`, `net/http` ServeMux (method patterns), sqlx (from gokit bootstrap), Redis, core NATS, PostgreSQL 15, nginx, Docker Compose, Coolify.

**Spec:** `docs/superpowers/specs/2026-10-18-go-project-design.md` (Part 2). Source being ported: `~/GitHub/assetpulse/assetpulse-app` (read-only).

## Global Constraints

- Repo `~/GitHub/go-project`; work on branch `feat/template`, never `main`.
- **Prerequisite:** gokit v0.4.0 is tagged and resolves on proxy.golang.org (gokit plan, Task 6). Every `go.mod` requires `github.com/piresc/gokit v0.4.0`; no `replace` directives.
- Module paths: `github.com/piresc/go-project/services/user-service`, `.../asset-service`, `.../price-service`. `go 1.26.0`.
- Ports: user-service `8084`, asset-service `8082`, price-service `8083`, nginx `8080`.
- Do not modify anything under `~/GitHub/assessment` or `~/GitHub/assetpulse`.
- Never copy `assetpulse-app/keys/`, `.env`, real domains (`app.example.com`), Traefik network IDs (`<traefik-network-id>`) or router names into go-project.
- No Google Wire, no chi, no `log.Printf` (use the slog logger from bootstrap), no `services/pkg/auth`.
- JSON responses use gokit `response.Success` / `response.Error` (`{"status":"success","data":...}` / `{"status":"error","error":"..."}`).
- Auth middleware: `middleware.RS256Auth(verifier, middleware.RS256Options{Revocation: auth.NewRedisRevocationStore(app.Redis)})` — fail-closed default.
- Token issuer default `user-service`; ACCESS_TOKEN_TTL `15m`, REFRESH_TOKEN_TTL `720h`.
- Every task ends with `go build ./... && go vet ./... && gofmt -l .` clean in each module it touched (`GOWORK` on).

## Review Focus

- `PATCH /users/me` with an unknown JSON field, empty body or a display name of only whitespace must return 400, not 500 or a silent write — test in Task 4.
- `POST /auth/refresh` reusing a refresh token that was already rotated must return 401 `invalid refresh token` (rotation must revoke the old one) — test in Task 2.
- Logout followed by any protected call with the same access token must return 401 `token revoked` — test in Task 4 (router test) and Task 5 (e2e, user-service routes; asset-service is not started by default compose).
- Startup with the private key missing must fail fast with an error naming `JWT_PRIVATE_KEY_PEM`/`JWT_PRIVATE_KEY_PATH`, not panic on first sign — test in Task 4.
- Redis profile-cache failure on `GET /users/me` must fall back to the database (cache is best-effort, as in uss), unlike revocation which is fail-closed — test in Task 2.

---

### Task 1: Repo root and skeleton services

**Files:**
- Create: `README.md`, `AGENTS.md`, `Makefile`, `.gitignore`, `.dockerignore`, `go.work`, `docs/ARCHITECTURE.md`
- Create: `services/asset-service/{go.mod,go.sum,cmd/server/main.go}`, `internal/app/{app.go,config.go}`, `internal/domain/asset.go`, `internal/usecase/{asset.go,ports.go,errors.go}`, `internal/repository/postgres/asset_repository.go`, `internal/gateway/cache/redis.go`, `internal/gateway/market/market.go`, `internal/gateway/messaging/nats.go`, `internal/handler/http/{router.go,asset_handler.go}`, `migrations/{embed.go,000001_init.up.sql,000001_init.down.sql}`, `config/local/asset-service.yaml`, `config/production/asset-service.yaml`
- Create: `services/price-service/` — same as asset-service minus `repository/` and `migrations/` (PSS has no DB); domain file `price.go`, usecase `price.go`, handler `price_handler.go`.

**Interfaces:**
- Produces (pattern every service follows; user-service copies it in Task 4):
  - `cmd/server/main.go`: `func main()` → `app.New()` → `a.Run()`; on error `os.Exit(1)` after logging to stderr. ≤ 25 lines.
  - `internal/app`: `type Config struct{...}` (service-specific `T`), `type App struct`, `func New() (*App, error)`, `func (a *App) Run() error`.
  - `New()` calls `bootstrap.NewHTTPService[Config]("<service-name>")`, loads `auth.LoadRSAPublicKey(cfg.Auth.PublicKeyPEM, cfg.Auth.PublicKeyPath)`, builds `middleware.NewMiddleware(app.Logger, app.Tracer)`, calls `httptransport.RegisterRoutes(app.Mux, ...)`.
  - `migrations/embed.go`: `package migrations; //go:embed *.sql; var FS embed.FS`; app calls `bootstrap.RunMigrations(migrations.FS, ".", cfg.Database)`.
- Skeleton routes: asset-service `GET /assets` behind `RS256Auth` → `response.Success(w, 200, []any{})`; price-service `GET /prices/top-performers` behind `OptionalRS256Auth` (guest-facing, like pss) → `response.Success(w, 200, []any{})`. Both fail in `New()` if `app.Redis` is nil (the revocation store needs it).
- Every other skeleton file: package doc comment + one placeholder (`type Asset struct{ ID string }`, an empty `AssetRepository interface{}` in `ports.go`, `ErrNotImplemented = errors.New("not implemented")`, constructor returning the zero adapter). No business logic.

- [ ] **Step 1 (orchestrator, before delegating):** in `~/GitHub/go-project` commit the spec + this plan on `main`, then `git switch -c feat/template`.
- [ ] **Step 2: Root files.**
  - `.gitignore`: `.env`, `*.pem`, `keys/`, `bin/`, `/services/*/server`, `*.out`, `*.cov`, `coverage*`, `.DS_Store`.
  - `go.work`: `go 1.26.0`, `use` the three service dirs (user-service added in Task 2).
  - `Makefile` targets: `test` (`go test -race -count=1 ./...` in each module), `vet`, `lint` (`golangci-lint run` per module), `build` (each `cmd/server` → `bin/<service>`), `keys` (`openssl genrsa -out keys/private.pem 2048 && openssl rsa -in keys/private.pem -pubout -out keys/public.pem`), `up`/`down` (compose, Task 5), `seed`, `e2e` (Task 5).
  - `docs/ARCHITECTURE.md`: layering rules adapted from `assetpulse-app/openwiki/architecture.md` (layers, "handlers are transport-only", "ports live in `usecase/ports.go`", "`cmd/server/main.go` is thin"), with Wire replaced by "hand wiring in `internal/app` on top of gokit bootstrap", event table `user.created` (user-service → asset-service).
  - `README.md`: what the template is, service map, how to start a new service by copying a skeleton, local run (`make keys`, `docker compose -f deploy/docker-compose.yml up`), Coolify pointer. `AGENTS.md`: points to `docs/ARCHITECTURE.md`.
- [ ] **Step 3: Skeleton services** per Files/Interfaces. `config/local/*.yaml`: `app.name`, `server.port`, `logger`, plus `database`/`redis` (asset-service) or `redis` (price-service) at localhost; `config/production/*.yaml`: same keys, hosts `postgres`/`redis`/`nats`, values overridable by env.
- [ ] **Step 4: Verify**
  Run in each skeleton dir: `go build ./... && go vet ./... && gofmt -l .` → clean. `go list ./...` lists every package in the Files list.
- [ ] **Step 5: Commit** `feat: repo root and asset/price service skeletons`

### Task 2: user-service domain and use cases

**Files:**
- Create: `services/user-service/{go.mod,go.sum}`, `internal/domain/models.go`, `internal/usecase/{ports.go,errors.go,auth.go,user.go,auth_test.go,user_test.go}`
- Modify: `go.work` (add `./services/user-service`)

Port from `assetpulse-app/services/uss/internal/{domain,usecase}`, preserving behavior (find-or-create with concurrent provider/email lookup, provider linking, refresh rotation, logout, cache-aside profile, `user.created` publish on create). Replace `log.Printf` with an injected `*slog.Logger`.

**Interfaces:**
- Produces `internal/domain`: `User`, `AuthProvider`, `RefreshToken`, `ProfileUpdate`, `TokenPair`, `OAuthIdentity`, `UserCreatedEvent` — fields and JSON tags exactly as uss `domain/models.go`.
- Produces `internal/usecase/ports.go`:
  - `UserRepository` — exactly uss's 10 methods.
  - `UserCache { GetUser(ctx, userID string) (*domain.User, bool, error); SetUser(ctx, *domain.User) error; DeleteUser(ctx, userID string) error }` (revocation removed from the cache).
  - `TokenRevoker { Revoke(ctx context.Context, jti string, ttl time.Duration) error }` — satisfied by `*auth.RedisRevocationStore`.
  - `EventPublisher`, `OAuthVerifier`, `TokenService` — exactly uss signatures.
- Produces `internal/usecase/errors.go`: uss's six errors plus `ErrInvalidInput = errors.New("invalid input")`.
- Produces: `func NewAuthUseCase(repo UserRepository, cache UserCache, revoker TokenRevoker, publisher EventPublisher, google, apple OAuthVerifier, tokens TokenService, accessTTL time.Duration, log *slog.Logger) *AuthUseCase` with methods `AuthenticateGoogle(ctx, idToken string) (*domain.TokenPair, error)`, `AuthenticateApple(...)`, `RefreshTokens(ctx, refreshToken string) (*domain.TokenPair, error)`, `Logout(ctx, userID, accessTokenJTI string, tokenExp time.Time) error`.
- Produces: `func NewUserUseCase(repo UserRepository, cache UserCache, log *slog.Logger) *UserUseCase` with `GetProfile(ctx, userID string) (*domain.User, error)`, `UpdateProfile(ctx, userID string, update domain.ProfileUpdate) (*domain.User, error)`.
- `Logout`: revoke all refresh tokens (error returned), then `revoker.Revoke(jti, time.Until(exp))`; a revoke error is logged at Warn and Logout still returns nil (uss behavior).
- `UpdateProfile` (new validation; uss has none — planner's call): `display_name` trimmed before saving; nil, empty after trim, or > 100 runes → `ErrInvalidInput`, nothing written.

- [ ] **Step 1: Port uss `auth_test.go` and `user_test.go`** onto the new constructors (hand-written fakes in the test files), and add:
  - `TestRefreshTokens_RotatedTokenRejected`: refresh with T1 → pair with T2; refresh with T1 again → `ErrInvalidRefresh`.
  - `TestLogout_RevokesAccessJTIWithRemainingTTL`: fake revoker records `(jti, ttl)`; ttl within 1s of `time.Until(exp)`.
  - `TestLogout_RevokerErrorStillSucceeds`: revoker returns error → Logout returns nil.
  - `TestGetProfile_CacheErrorFallsBackToRepo`: cache `GetUser` returns error → profile from repo, no error.
  - `TestUpdateProfile_BlankNameRejected`: `"   "` → `ErrInvalidInput`.
- [ ] **Step 2: Run** `go test ./internal/usecase/ -v` → FAIL (undefined).
- [ ] **Step 3: Implement** domain, ports, errors, use cases.
- [ ] **Step 4: Run** `go test -race ./internal/... -v` → PASS.
- [ ] **Step 5: Commit** `feat(user-service): domain and use cases ported from uss`

### Task 3: user-service adapters

**Files:**
- Create: `internal/repository/postgres/{user_repository.go,user_repository_test.go}`
- Create: `migrations/{embed.go,000001_create_tables.up.sql,000001_create_tables.down.sql}`
- Create: `internal/gateway/cache/{redis.go,redis_test.go}`
- Create: `internal/gateway/messaging/{nats.go,nats_test.go}`
- Create: `internal/gateway/oauth/{google.go,apple.go,claims.go,errors.go,*_test.go}`
- Create: `internal/gateway/token/{token.go,token_test.go}`

**Interfaces:**
- Consumes: Task 2 ports; gokit `auth.RSASigner`, `redis.Client`, `nats.Client`, `testkit` containers.
- Produces:
  - `postgres.NewUserRepository(db *sqlx.DB) *UserRepository` — implements `usecase.UserRepository`; same SQL as uss `repository/postgres/user_repository.go`, rewritten from pgx to sqlx (`$n` placeholders, `GetContext`/`ExecContext`), `sql.ErrNoRows` → `(nil, nil)` where uss returns nil for not found. Every nullable or computed column gets an explicit alias so sqlx can map it: `COALESCE(display_name,'') AS display_name`, `COALESCE(avatar_url,'') AS avatar_url`.
  - `migrations/000001_create_tables.up.sql` = `assetpulse-app/deploy/bootstrap/uss/001_create_tables.sql` verbatim; `.down.sql` drops `refresh_tokens`, `auth_providers`, `users`. `embed.go` as in Task 1.
  - `cache.NewRedisCache(client *redis.Client) *RedisCache` (gokit client; use `client.GetClient()`) — key `user:<id>`, TTL 5m, `redis.Nil` → `(nil,false,nil)`.
  - `messaging.NewNATSPublisher(client *nats.Client) *NATSPublisher` — `PublishUserCreated` marshals the event and calls `client.Conn().Publish("user.created", data)` (core NATS).
  - `oauth.NewGoogleVerifier(clientID string) *GoogleVerifier`, `oauth.NewAppleVerifier(clientID string) *AppleVerifier` — each with `Verify(ctx, idToken) (*domain.OAuthIdentity, error)` and `WarmCache(ctx) error`. Merge uss `internal/oauth/*` (real JWKS logic) and `internal/gateway/oauth/*` (adapter) into one implementation; port the tests that cover kept behavior (`google_test`, `apple_test`, property and bugfix tests that still apply).
  - `token.NewService(signer *auth.RSASigner, refreshTTL time.Duration) *Service` — implements `usecase.TokenService`: `GenerateAccessToken(user)` → `signer.Sign(user.ID, user.Email, user.Role)`; `GenerateRefreshToken()` → 32 random bytes hex plaintext, SHA-256 hex hash, `now+refreshTTL` (same as uss `internal/token`).

- [ ] **Step 1: Write failing tests**
  - Repository (testkit Postgres container; build `config.DatabaseConfig` from the container's `Host`/`MappedPort` and credentials, since testkit exposes no `DatabaseConfig`; run migrations via `bootstrap.RunMigrations(migrations.FS, ".", cfg)`; open `*sqlx.DB` with `database.NewPostgresClient(cfg)`): user with NULL `display_name`/`avatar_url` reads back as empty strings; create/find by id/email/provider, `CreateRefreshToken`→`FindRefreshTokenByHash`, `RevokeRefreshToken`, `RevokeAllUserRefreshTokens`, `UpdateDisplayName`; unknown id → `nil, nil`.
  - Cache (miniredis): set/get round-trip, miss, delete, TTL 5m.
  - NATS: start an in-process server (`github.com/nats-io/nats-server/v2/test`) or skip with a clear message if that adds a heavy dep — builder call; subscriber receives JSON `{"user_id","email","role"}` on `user.created`.
  - Token: `GenerateRefreshToken` → `sha256(plaintext)==hash`, two calls differ; `GenerateAccessToken` verifies with `auth.NewRSAVerifier(pub)` and carries `Subject/Email/Role`.
  - Seed-hash check: `sha256("seed-refresh-token")` hex == `94ddae8dc4a371cbdcc42ca2635623330272847ec68495b572d155d9bae902f0` (keeps the Task 5 seed valid).
- [ ] **Step 2: Run** `go test ./internal/... -v` → FAIL.
- [ ] **Step 3: Implement adapters.**
- [ ] **Step 4: Run** `go test -race ./... -v` → PASS (Docker available).
- [ ] **Step 5: Commit** `feat(user-service): postgres, redis, nats, oauth and token adapters`

### Task 4: user-service HTTP and app wiring

**Files:**
- Create: `internal/handler/http/{router.go,auth_handler.go,user_handler.go,errors.go,dto.go,router_test.go,auth_handler_test.go,user_handler_test.go}`
- Create: `internal/app/{app.go,config.go,keys.go,keys_test.go}`, `cmd/server/main.go`
- Create: `config/local/user-service.yaml`, `config/production/user-service.yaml`

**Interfaces:**
- Consumes: Tasks 2–3; Task 1 app pattern.
- Produces:
  - `httptransport.RegisterRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler, auth *AuthHandler, user *UserHandler)` registering `POST /auth/google`, `POST /auth/apple`, `POST /auth/refresh` (public) and `POST /auth/logout`, `GET /users/me`, `PATCH /users/me` (wrapped in `authMW`).
  - Handler-side ports (consumer-defined, in `handler/http`): `Authenticator` (the four AuthUseCase methods), `ProfileReader`, `ProfileWriter`.
  - Request/response DTOs: same JSON field names as uss handlers (`id_token`, `refresh_token`, `display_name`; token pair and profile response as uss `toProfileResponse`). Decode with `DisallowUnknownFields`; bad/empty JSON → 400 `invalid request body`.
  - Error mapping (`errors.go`), status same as uss `writeUseCaseError`, gokit envelope, messages: `ErrInvalidToken`→401 `invalid token`; `ErrTokenExpired`→401 `token expired`; `ErrInvalidAudience`→401 `invalid token audience`; `ErrRefreshExpired`→401 `refresh token expired`; `ErrInvalidRefresh`→401 `invalid refresh token`; `ErrUserNotFound`→404 `user not found`; `ErrInvalidInput`→400 `invalid input`; other→500 `internal server error` (logged, cause never in body).
  - Logout reads `middleware.GetClaims(r)` for `Subject`, `ID`, `ExpiresAt`.
  - `app.Config{ GoogleClientID string \`mapstructure:"google_client_id"\`; AppleClientID string \`mapstructure:"apple_client_id"\` }` read from YAML `service.google_client_id` / `service.apple_client_id`; in `New()`, non-empty env `GOOGLE_CLIENT_ID` / `APPLE_CLIENT_ID` overrides them (same env names as AssetPulse).
  - `func loadKeys(cfg config.AuthConfig) (*rsa.PrivateKey, *rsa.PublicKey, error)` in `internal/app/keys.go` — private-key error wrapped as `load JWT private key (JWT_PRIVATE_KEY_PEM or JWT_PRIVATE_KEY_PATH): %w`, public as `load JWT public key (JWT_PUBLIC_KEY_PEM or JWT_PUBLIC_KEY_PATH): %w`. Testable without bootstrap.
  - `app.New()`: bootstrap → require `app.DB`, `app.Redis`, `app.NATSClient` non-nil (else error naming the missing setting) → `loadKeys(cfg.Auth)` → `RunMigrations` → OAuth verifiers with `WarmCache` (failure logged at Warn, not fatal) → adapters → use cases → handlers → `RegisterRoutes`. Issuer = `cfg.Auth.Issuer`, default `user-service` when empty.

- [ ] **Step 1: Write failing tests**
  - `router_test.go` (fake use cases, real `RS256Auth` with testkit keys + miniredis revocation): each protected route without token → 401 `authentication required`; with a revoked jti → 401 `token revoked`; public routes reachable without token; `GET /users/unknown-path` → 404.
  - `user_handler_test.go`: `PATCH /users/me` with `{}`→400, `{"display_name":"   "}`→400, `{"display_name":"x","extra":1}`→400, valid → 200 with updated profile.
  - `auth_handler_test.go`: `POST /auth/google` `{"id_token":"invalid-token"}` with fake returning `ErrInvalidToken` → 401 `invalid token`; refresh happy path → 200 token pair; logout passes claims' jti/exp to the use case.
  - `app/keys_test.go`: `loadKeys` with empty `AuthConfig` → error containing `JWT_PRIVATE_KEY_PEM`; with testkit PEMs → both keys non-nil; private set, public missing → error containing `JWT_PUBLIC_KEY_PEM`.
- [ ] **Step 2: Run** `go test ./internal/handler/... ./internal/app/ -v` → FAIL.
- [ ] **Step 3: Implement** handlers, router, app, `cmd/server/main.go`, config YAMLs (`server.port: 8084`, `nats.enabled: true`, `auth.issuer: user-service`).
- [ ] **Step 4: Run** `go test -race ./... && go vet ./... && golangci-lint run` → PASS, `0 issues`.
- [ ] **Step 5: Commit** `feat(user-service): HTTP handlers and app wiring on gokit bootstrap`

### Task 5: Deployment (local + Coolify) and e2e

**Files:**
- Create: `services/{user-service,asset-service,price-service}/Dockerfile`
- Create: `deploy/{docker-compose.yml,docker-compose.coolify.yml,nginx.conf,Dockerfile.nginx,Dockerfile.postgres,.env.example,runtime.coolify.env.example,e2e_test.sh}`, `deploy/bootstrap/{init-db.sh,seed-test-account.sql}`
- Modify: `Makefile` (`up`, `down`, `seed`, `e2e`), `README.md` (local run, Coolify checklist adapted from `assetpulse-app/docs/DEPLOY_COOLIFY.md`, key generation)

**Interfaces:**
- Dockerfile (each): `golang:1.26-alpine` builder, context = the service dir, `ENV GOWORK=off`, copy `go.mod go.sum` → `go mod download` → copy source → `CGO_ENABLED=0 go build -o /app/server ./cmd/server`; runtime `alpine` with `ca-certificates` and `wget` (busybox, for the healthcheck), `WORKDIR /app`, copies `/app/server` and `config/` to `/app/config/` (bootstrap reads `config/<APP_ENV>/<name>.yaml` relative to the working dir), `ENV APP_ENV=production`, `EXPOSE <port>`.
- `docker-compose.yml` (local): `nginx` (8080:8080), `user-service`, `postgres` (built from `Dockerfile.postgres`, healthcheck), `redis` (healthcheck), `nats`; asset-service and price-service blocks present but commented. user-service has `healthcheck: wget -qO- http://localhost:8084/health/ready` and nginx `depends_on: user-service: condition: service_healthy`. user-service env: `DB_HOST=postgres DB_PORT=5432 DB_USERNAME DB_PASSWORD DB_DATABASE=users DB_SSL_MODE=disable REDIS_HOST=redis REDIS_PORT=6379 NATS_URL=nats://nats:4222 JWT_PRIVATE_KEY_PATH=/keys/private.pem JWT_PUBLIC_KEY_PATH=/keys/public.pem` with `../keys` mounted read-only at `/keys`, plus TTLs and OAuth client IDs from `.env`.
- `docker-compose.coolify.yml`: port of `assetpulse-app/deploy/docker-compose.coolify.yml` without `frontend`; build contexts written relative to the repo root as AssetPulse does (Coolify runs compose from the repo root): `context: ./deploy` for nginx/postgres, `context: ./services/<name>` for services; nginx Traefik labels use `${APP_DOMAIN:?}`, `${TRAEFIK_NETWORK:?}` and router/service names `${APP_NAME:-go-project}-…`; keys via `JWT_PRIVATE_KEY_PEM`/`JWT_PUBLIC_KEY_PEM` env; asset/price blocks commented. Keep AssetPulse's comments about Cloudflare Flexible mode and router priority.
- `nginx.conf`: AssetPulse's file minus frontend and SPA fallback; `/api/v1/auth*` and `/api/v1/users*` → `user-service:8084` with the same rewrites; asset/price locations kept but commented; rate limit and security headers kept; CORS headers removed (gokit's `CorsHandler` already sets them — duplicates break browsers).
- `Dockerfile.postgres`: `postgres:15.14-bookworm`, copies only `init-db.sh` (creates databases `users` and `assets`; no migrations — services run their own).
- Seed: `seed-test-account.sql` ported from AssetPulse, but the refresh-token insert becomes `ON CONFLICT (id) DO UPDATE SET revoked = FALSE, expires_at = NOW() + INTERVAL '30 days'` so e2e can be re-run (its logout revokes the token). `make up` = `docker compose ... up -d --build --wait` (waits for healthchecks, so migrations have run). `make seed` = `docker compose -f deploy/docker-compose.yml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d users' < deploy/bootstrap/seed-test-account.sql` (the variable expands inside the container). README: never seed production.
- `e2e_test.sh`: port of AssetPulse's script (keep its `SEEDED_REFRESH_TOKEN` default `seed-refresh-token` and `BASE_URL` default `http://localhost:8080`) keeping only auth/user steps (invalid Google/Apple token → 401, refresh with `seed-refresh-token` → 200, `/users/me` without token → 401, with token → 200, `PATCH /users/me`, logout → then `/users/me` with same token → 401); expected bodies updated to the gokit envelope; all asset/price steps removed.
- `.env.example` / `runtime.coolify.env.example`: every variable used above, placeholder values only, `SEED_TEST_ACCOUNT` removed (seed is now `make seed`).

- [ ] **Step 1:** Write Dockerfiles, compose files, nginx, bootstrap scripts, env examples, Makefile targets, README sections.
- [ ] **Step 2: Validate config**
  Run: `docker compose -f deploy/docker-compose.yml config -q` and, from the repo root, `APP_DOMAIN=x TRAEFIK_NETWORK=y docker compose --project-directory . -f deploy/docker-compose.coolify.yml config -q` (placeholder env) → exit 0, and every resolved `build.context` exists.
  Run: `grep -rnE 'assetpulse\.piresc\.dev|<traefik-network-id>|-----BEGIN [A-Z ]*PRIVATE KEY-----' --exclude-dir=.git .` → no matches (the `.env.example` placeholders must use `...` without a BEGIN line, or the grep excludes `*.example` explicitly).
- [ ] **Step 3: Build images** `docker compose -f deploy/docker-compose.yml build` → success (asset/price images: `docker build services/asset-service` and `services/price-service` → success).
- [ ] **Step 4: Run e2e** `make keys && make up && make seed && make e2e` → all steps PASS; run `make seed && make e2e` a second time → PASS again; then `make down`.
- [ ] **Step 5: Commit** `feat: local and Coolify deployment, e2e for user-service`

### Task 6: Final verification (read-only)

- [ ] **Step 1:** In each module: `go build ./... && go vet ./... && go test -race -count=1 ./... && gofmt -l . && golangci-lint run && go mod tidy -diff` → all clean.
- [ ] **Step 2:** `git ls-files | grep -E '\.pem$|(^|/)\.env$|(^|/)server$'` → no output.
- [ ] **Step 3:** Orchestrator writes `git diff main..feat/template` to the scratchpad and sends it with the spec to `code-reviewer`; fixes go back to the implementer; then merge `feat/template` into `main` locally (no remote yet — creating a GitHub repo and pushing is a separate decision for the user).
