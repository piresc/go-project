# Architecture

## Overview

go-project is a set of Go services built on `github.com/piresc/gokit`. The stack runs one PostgreSQL database with a schema and a login role per service, and one Redis with a DB number per service. Services talk to each other through NATS JetStream for async events and through HTTP for synchronous reads. NATS, Postgres and Redis are shared containers, but never shared data: each service touches only its own schema and Redis DB. All infrastructure is wired once in `pkg/platform` and switched on by config.

## Service Boundaries

| Service | Responsibility | Port | Schema (role) | Redis DB |
|---------|---------------|------|---------------|----------|
| **user-service** | Authentication, user profiles, token management | 8084 | `users` (`user_svc`) | 1 |
| **asset-service** | Asset CRUD, per-user portfolio, holdings and valuation | 8082 | `assets` (`asset_svc`) | 2 |
| **price-service** | Scheduled quote refresh, top performers, quotes | 8083 | None | 0 |

All schemas live in the database `app` on the `postgres` container; all Redis DBs on the `redis` container.

A service never reads another service's schema or Redis DB. Postgres enforces this: each role owns its schema, has its `search_path` set to it, and has no access to the others, so a cross-schema read fails with "permission denied". Redis isolation is by DB number only, which is a convention (no ACLs, because gokit has no Redis username).

One Go module (`go.mod` at the repo root, `github.com/piresc/go-project`). Each service has an entrypoint under `cmd/<name>-service/` and its own tree under `internal/<svc>/` (`user`, `asset`, `price`). One `deploy/Dockerfile` builds any service via the `SERVICE` and `PORT` build args.

## Auth

Tokens are RS256 JWTs. user-service signs them with the private key; every other service only holds the public key. `platform.App` builds `Auth` (and `OptionalAuth` for guest-readable routes) with a revocation store on the service's own Redis, so revoked token IDs are rejected, unless `service.auth.skip_revocation_check` is set (see below). There is no shared auth package inside this repo: gokit provides it.

user-service revokes the token ID on logout (in its own Redis DB, 1) and enforces it. asset-service and price-service set `service.auth.skip_revocation_check: true`: they do not read user-service's Redis, so a revoked token is accepted there until it expires (`ACCESS_TOKEN_TTL`, 15m by default). With the flag set, `Revocations` is nil and a public key needs no Redis.

## Layout

```
cmd/<name>-service/  main.go: thin entrypoint
config/<env>/        <name>-service.yaml per environment (local, production)
pkg/platform/        the one place infrastructure is wired
internal/<svc>/
  app/          service Config and short wiring on top of platform.New
  domain/       Core entities, value objects, validation
  usecase/      Business logic, port interfaces, typed errors
  repository/   Postgres adapters implementing repository ports
  gateway/      Redis, market-data, lock adapters
  handler/      http/ routes and DTOs; event/ and task/ handlers
  migrations/   Embedded SQL (services with a database)
```

Design rules:
- **Handlers are transport-only**: no business logic in the HTTP, event or task layer.
- **Ports live in `usecase/ports.go`**: use cases depend on interfaces defined there.
- **Adapters implement ports**: repositories and gateways are easy to substitute in tests.
- **Infrastructure is wired only in `pkg/platform`**: `internal/<svc>/app/app.go` calls `platform.New[Config]`, builds adapters and use cases, and registers handlers. No code generation.
- **`cmd/<name>-service/main.go` is thin**: it calls `app.New()` then `Run()` and exits non-zero on error.

## Platform capabilities

`platform.New[S](name, opts...)` wraps `bootstrap.NewHTTPService` and returns `*platform.App[S]`, which embeds the bootstrap app (DB, Redis, NATS, Logger, Mux, Config). `S` is the service's own config, read from the YAML `service:` block. Options: `platform.WithMigrations(fs, dir)` and `platform.Require(...)` with `platform.Postgres`, `Redis`, `NATS`, `Asynq`, `AuthKeys`, which fails startup with a hint when a dependency is missing.

| Field / method | Built when (YAML switch) | What it is |
|---|---|---|
| `Service S` | always (`service:`) | the service's own settings |
| `Middleware` | always | gokit middleware (e.g. `RequireRole`) |
| `Auth`, `OptionalAuth` | `auth.public_key_path` or `JWT_PUBLIC_KEY_PEM` | RS256 middleware with Redis revocation (skipped when `service.auth.skip_revocation_check: true`); fail-closed (503) unless `service.auth.fail_open: true` |
| `Revocations` | Redis configured (`redis.host`) and `service.auth.skip_revocation_check` not true | `auth.RedisRevocationStore` on `Redis`; nil when the check is skipped |
| `Events` | `nats.enabled` | JetStream: streams and consumers from `service.events`; JSON `Publish(ctx, subject, v, msgID)` with dedup, `Subscribe(consumer, handler)` |
| `Tasks` | `asynq.enabled` (needs `redis.db: 0`) | asynq `Enqueue`, `Handle(type, fn)`, cron schedules from `service.tasks.schedules` |
| `Locker` | Redis configured; `service.lock.ttl` (default 30s) | gokit redislock, key prefix = service name, no retry |
| `HTTPClient(name)` | `service.http_clients.<name>` (`base_url`, `timeout`, `failure_threshold`, `open_timeout`) | gokit httpclient (up to 3 attempts, trace headers) behind a circuit breaker; `GetJSON` unwraps the gokit `response.Success` envelope and returns a `*httpclient.APIError` (status, code, message) for 4xx/5xx and `success:false` bodies; returns `ErrUnavailable` while the breaker is open |

Scheduled tasks: each schedule fires on wall-clock-aligned ticks and enqueues with TaskID `<task>:<tick unix>`, `Retention(interval)` and `MaxRetry(0)`. N replicas compute the same ID, so each tick runs once; a failed run waits for the next tick. Every scheduled task needs a `Handle` before `Run`. Every consumer in `service.events.consumers` needs a `Subscribe` before `Run`, otherwise `Run` fails.

`Run()` starts subscriptions, the task worker and the schedules, then serves HTTP. On SIGINT/SIGTERM HTTP drains first. Then the schedules stop first, then the task worker, then the event consumers, and only then DB, Redis and NATS close.

## Service calls (HTTP)

- Calls use `platform.HTTPClient` from `service.http_clients.<name>`. The caller owns a `gateway/<callee>` adapter behind a usecase port, with its own domain types; it does not import the callee's packages. The adapter takes a narrow `GetJSON` interface that `*platform.HTTPClient` satisfies.
- Calls go container to container (for example `http://price-service:8083`), not through nginx. Production can override the base URL with `SERVICE_HTTP_CLIENTS_<NAME>_BASE_URL`.
- The client makes up to 3 attempts per call, each limited by the client's `timeout`, behind a circuit breaker. While the breaker is open, `GetJSON` returns `ErrUnavailable` without calling the upstream. The usecase maps any upstream failure to a domain error (`ErrPricesUnavailable`) and the handler answers 503.
- The internal call is unauthenticated: the platform client cannot forward the caller's token. A callee must expose only public data on such routes (`GET /prices/quotes` uses `OptionalAuth`).
- Keep `timeout` x 3 attempts under nginx's 5s `proxy_read_timeout`, so an outage gives 503, not 504.

| Caller | Callee | Call | Used for |
|--------|--------|------|----------|
| asset-service (`service.http_clients.price`) | price-service | `GET /prices/quotes?symbols=A,B` | `GET /portfolio/value` |

## HTTP endpoints

Every JSON response uses the gokit envelope, the same format as AssetPulse:

```json
{"success": true, "data": {"...": "..."}}
{"success": false, "error": {"code": "ERR_NOT_FOUND", "message": "Portfolio not found"}}
```

`data` is left out when empty, so an empty list gives `{"success":true}`; clients must treat a missing `data` as empty. Handlers call `response.Success(w, status, data)` and `response.Error(w, status, code, message)`. Use gokit's `response.Code*` constants where one fits. A service-specific code is a constant in the service's `handler/http/errors.go` (`ERR_TOO_MANY_HOLDINGS` in asset-service; `ERR_INVALID_TOKEN`, `ERR_INVALID_AUDIENCE`, `ERR_REFRESH_EXPIRED`, `ERR_INVALID_REFRESH` in user-service). Messages are sentence case.

| Service | Route | Auth | Notes |
|---------|-------|------|-------|
| user-service | `POST /auth/google` | public | |
| user-service | `POST /auth/apple` | public | |
| user-service | `POST /auth/refresh` | public | body `{"refresh_token": "..."}` |
| user-service | `POST /auth/logout` | required | |
| user-service | `GET /users/me` | required | |
| user-service | `PATCH /users/me` | required | |
| asset-service | `GET /assets` | required | |
| asset-service | `GET /portfolio` | required | portfolio plus `holdings` |
| asset-service | `PUT /portfolio/holdings/{symbol}` | required | body `{"quantity": n}`; creates the portfolio if missing; 400 invalid, 409 over 50 holdings |
| asset-service | `DELETE /portfolio/holdings/{symbol}` | required | 204; 404 if missing |
| asset-service | `GET /portfolio/value` | required | holdings priced via price-service; `unpriced` lists symbols without a quote; 503 if prices are unavailable |
| price-service | `GET /prices/top-performers` | optional | |
| price-service | `GET /prices/quotes?symbols=A,B` | optional | cached quotes only, in request order; unknown symbols omitted; 1 to 50 symbols, else 400 |

## Add a use case

Follow these steps; the examples are real code to copy.

1. **Domain type**: `internal/<svc>/domain/`. Example: `internal/asset/domain/portfolio.go`.
2. **Port**: interface in `internal/<svc>/usecase/ports.go`. Example: `internal/asset/usecase/ports.go`.
3. **Use case plus test with fakes**: `internal/<svc>/usecase/`. Examples: `internal/asset/usecase/portfolio.go`, `internal/price/usecase/refresh.go`.
4. **Adapter plus migration**: Postgres in `repository/postgres/`, everything else in `gateway/`. Examples: `internal/asset/repository/postgres/portfolio_repository.go` with `internal/asset/migrations/000002_portfolios.{up,down}.sql`; `internal/price/gateway/market` (HTTP client), `internal/price/gateway/lock` (Locker), `internal/price/gateway/cache`.
5. **Handler**, transport-only:
   - HTTP route: `handler/http/router.go`, `RegisterRoutes`. Example: `internal/asset/handler/http/portfolio_handler.go`.
   - Event: `handler/event/`, registered with `Events.Subscribe`. Example: `internal/asset/handler/event/user_created.go`.
   - Task: `handler/task/`, registered with `Tasks.Handle`. Example: `internal/price/handler/task/refresh.go`.
   - Needs another service's data: add a gateway (see "Service calls (HTTP)") plus a `service.http_clients.<name>` entry. Never read that service's schema or Redis DB. Example: `internal/asset/gateway/price`.
6. **One wiring line** in `internal/<svc>/app/app.go`: build the adapter and use case, then register the handler (`app.Events.Subscribe(...)`, `app.Tasks.Handle(...)`, `httptransport.RegisterRoutes(...)`). Examples: `internal/asset/app/app.go` (events, migrations), `internal/price/app/app.go` (tasks, lock, `HTTPClient`). Add the needed dependency to `platform.Require`.
7. **Config keys**: in `config/local/<svc>.yaml` and `config/production/<svc>.yaml`: `nats.enabled` plus `service.events` (streams, consumers), `asynq.enabled` plus `service.tasks.schedules`, `service.http_clients.<name>`, `service.lock.ttl`, `service.auth.fail_open`, `service.auth.skip_revocation_check`, `auth.public_key_path`. Service-specific keys go in `internal/<svc>/app/config.go`.

## Event Bus (NATS JetStream)

| Subject | Publisher | Consumer | Payload |
|---------|-----------|----------|---------|
| `user.created` | user-service | asset-service (durable consumer `asset-service-user-created` on stream `USERS`; creates the user's portfolio exactly once) | `{user_id, email, role}` |

## Caching Strategy (Redis)

One Redis container; each service has its own DB number (price 0, user 1, asset 2).

At most one service may enable asynq per Redis. asynq requires DB 0 and all asynq services share it and the default queue, so a second one would take price-service's tasks.

- **user-service** (DB 1): revoked access-token JTI set (TTL = token remaining life). Only user-service uses it.
- **price-service** (DB 0): latest quotes under one key with no TTL, so the last good quotes survive a market outage; refreshed by the `price:refresh` task under a lock. `GET /prices/quotes` reads only this cache, never the market.
- **asset-service** (DB 2): wired for platform needs only; no application keys yet.

## Migrations

Services with a database embed their SQL in `internal/<svc>/migrations/` (`embed.FS`) and apply them at boot through `platform.WithMigrations`. Each service migrates only its own schema. `deploy/bootstrap/init-db.sh` creates each service's role and schema on a fresh volume; the tables come from these migrations. SQL uses unqualified table names, resolved through the role's `search_path`, and golang-migrate's `schema_migrations` table lands in the service's own schema.

### Add a service with a database

1. Add the service's role and schema to `deploy/bootstrap/init-db.sh` by copying the whole `user_svc` block, including `GRANT CONNECT ON DATABASE` (a password env var such as `<NAME>_DB_PASSWORD`, `CREATE ROLE`, `GRANT CONNECT`, `CREATE SCHEMA ... AUTHORIZATION`, `ALTER ROLE ... SET search_path`).
2. Add the password env to the `postgres` service in both compose files and to the env examples, and give the service `DB_USERNAME`, `DB_PASSWORD`, `DB_DATABASE: app`. Do this before step 3: the script's `:?` guard aborts when the variable is missing.
3. On an existing volume, rebuild and run `make db-init` (locally) or `docker exec <postgres-container> bash /docker-entrypoint-initdb.d/00-init-db.sh` (Coolify). The script is idempotent. A fresh volume runs it automatically.
4. Set `database.*` and a free `redis.db` in `config/{local,production}/<svc>.yaml`.

## Key Source Files

| File | Purpose |
|------|---------|
| `go.mod` | The single module for all services |
| `pkg/platform/*.go` | Infrastructure wiring (auth, events, tasks, lock, HTTP clients) |
| `pkg/platform/httpclient.go` | HTTP client, breaker, gokit envelope decoding and `APIError` mapping |
| `pkg/platform/auth.go` | Auth middleware and the revocation store (optional) |
| `internal/*/app/app.go` | Wiring per service |
| `internal/asset/gateway/price/price.go` | Example of a service-call gateway |
| `deploy/docker-compose.yml` | Local stack: one Postgres (schema per service) and one Redis (DB per service) |
| `cmd/*/main.go` | Service entrypoints |
| `config/<env>/*.yaml` | Per-environment configuration |
| `deploy/Dockerfile` | Shared service image (`SERVICE`, `PORT` args) |
| `deploy/bootstrap/init-db.sh` | Idempotent per-service Postgres role and schema setup |
