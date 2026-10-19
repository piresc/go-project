# Architecture

## Overview

go-project is a set of Go services built on `github.com/piresc/gokit`. Services talk to each other through NATS JetStream for async events and share infrastructure (PostgreSQL, Redis). All infrastructure is wired once in `pkg/platform` and switched on by config.

## Service Boundaries

| Service | Responsibility | Port | Database |
|---------|---------------|------|----------|
| **user-service** | Authentication, user profiles, token management | 8084 | Postgres |
| **asset-service** | Asset CRUD, per-user portfolio | 8082 | Postgres |
| **price-service** | Scheduled quote refresh, top performers | 8083 | None (Redis only) |

One Go module (`go.mod` at the repo root, `github.com/piresc/go-project`). Each service has an entrypoint under `cmd/<name>-service/` and its own tree under `internal/<svc>/` (`user`, `asset`, `price`). One `deploy/Dockerfile` builds any service via the `SERVICE` and `PORT` build args.

## Auth

Tokens are RS256 JWTs. user-service signs them with the private key; every other service only holds the public key. `platform.App` builds `Auth` (and `OptionalAuth` for guest-readable routes) with a Redis revocation store, so revoked token IDs are rejected. There is no shared auth package inside this repo: gokit provides it.

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
| `Auth`, `OptionalAuth` | `auth.public_key_path` or `JWT_PUBLIC_KEY_PEM` | RS256 middleware with Redis revocation; fail-closed (503) unless `service.auth.fail_open: true` |
| `Revocations` | Redis configured (`redis.host`) | `auth.RedisRevocationStore` |
| `Events` | `nats.enabled` | JetStream: streams and consumers from `service.events`; JSON `Publish(ctx, subject, v, msgID)` with dedup, `Subscribe(consumer, handler)` |
| `Tasks` | `asynq.enabled` (needs `redis.db: 0`) | asynq `Enqueue`, `Handle(type, fn)`, cron schedules from `service.tasks.schedules` |
| `Locker` | Redis configured; `service.lock.ttl` (default 30s) | gokit redislock, key prefix = service name, no retry |
| `HTTPClient(name)` | `service.http_clients.<name>` (`base_url`, `timeout`, `failure_threshold`, `open_timeout`) | gokit httpclient (retries, trace headers) behind a circuit breaker |

Scheduled tasks: each schedule fires on wall-clock-aligned ticks and enqueues with TaskID `<task>:<tick unix>`, `Retention(interval)` and `MaxRetry(0)`. N replicas compute the same ID, so each tick runs once; a failed run waits for the next tick. Every scheduled task needs a `Handle` before `Run`.

`Run()` starts subscriptions, the task worker and the schedules, then serves HTTP. On SIGINT/SIGTERM HTTP drains first. Then the schedules stop first, then the task worker, then the event consumers, and only then DB, Redis and NATS close.

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
6. **One wiring line** in `internal/<svc>/app/app.go`: build the adapter and use case, then register the handler (`app.Events.Subscribe(...)`, `app.Tasks.Handle(...)`, `httptransport.RegisterRoutes(...)`). Examples: `internal/asset/app/app.go` (events, migrations), `internal/price/app/app.go` (tasks, lock, `HTTPClient`). Add the needed dependency to `platform.Require`.
7. **Config keys**: in `config/local/<svc>.yaml` and `config/production/<svc>.yaml`: `nats.enabled` plus `service.events` (streams, consumers), `asynq.enabled` plus `service.tasks.schedules`, `service.http_clients.<name>`, `service.lock.ttl`, `service.auth.fail_open`, `auth.public_key_path`. Service-specific keys go in `internal/<svc>/app/config.go`.

## Event Bus (NATS JetStream)

| Subject | Publisher | Consumer | Payload |
|---------|-----------|----------|---------|
| `user.created` | user-service | asset-service (durable consumer `asset-service-user-created` on stream `USERS`; creates the user's portfolio exactly once) | `{user_id, email, role}` |

## Caching Strategy (Redis)

- **user-service**: revoked access-token JTI set (TTL = token remaining life)
- **price-service**: latest quotes under one key with no TTL, so the last good quotes survive a market outage; refreshed by the `price:refresh` task under a lock

## Migrations

Services with a database embed their SQL in `internal/<svc>/migrations/` (`embed.FS`) and apply them at boot through `platform.WithMigrations`.

## Key Source Files

| File | Purpose |
|------|---------|
| `go.mod` | The single module for all services |
| `pkg/platform/*.go` | Infrastructure wiring (auth, events, tasks, lock, HTTP clients) |
| `internal/*/app/app.go` | Wiring per service |
| `cmd/*/main.go` | Service entrypoints |
| `config/<env>/*.yaml` | Per-environment configuration |
| `deploy/Dockerfile` | Shared service image (`SERVICE`, `PORT` args) |
