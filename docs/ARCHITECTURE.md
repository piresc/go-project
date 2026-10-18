# Architecture

## Overview

go-project is a set of independent Go services built on `github.com/piresc/gokit`. Services talk to each other through NATS for async events and share infrastructure (PostgreSQL, Redis).

## Service Boundaries

| Service | Responsibility | Port | Database |
|---------|---------------|------|----------|
| **user-service** | Authentication, user profiles, token management | 8084 | Postgres |
| **asset-service** | Asset CRUD | 8082 | Postgres |
| **price-service** | Prices, top performers | 8083 | None (Redis only) |

All services share one Go module (`go.mod` at the repo root). Each has an entrypoint under `cmd/<name>-service/` and its own tree under `internal/<svc>/` (`user`, `asset`, `price`). One `deploy/Dockerfile` builds any service via the `SERVICE` and `PORT` build args.

## Auth

Tokens are RS256 JWTs. user-service signs them with the private key; every other service only holds the public key and verifies with `auth.NewRSAVerifier`. Services install `middleware.RS256Auth` (or `OptionalRS256Auth` for guest-readable routes) with a `RedisRevocationStore`, so revoked token IDs are rejected. There is no shared auth package inside this repo: gokit provides it.

## Internal Layering (per service)

```
cmd/<name>-service/  main.go: thin entrypoint
config/<env>/        <name>-service.yaml per environment
internal/<svc>/
  app/          Config, hand wiring on top of gokit bootstrap, lifecycle
  domain/       Core entities, value objects, validation
  usecase/      Business logic, port interfaces, typed errors
  repository/   Postgres adapters implementing repository ports
  gateway/      Redis, NATS, market-data adapters
  handler/http/ HTTP transport: routes, DTOs, error mapping
  migrations/   Embedded SQL (services with a database)
```

Design rules:
- **Handlers are transport-only**: no business logic in the HTTP layer.
- **Ports live in `usecase/ports.go`**: use cases depend on interfaces defined there.
- **Adapters implement ports**: repositories and gateways are easy to substitute in tests.
- **Hand wiring in `internal/app` on top of gokit bootstrap**: `New()` calls `bootstrap.NewHTTPService[Config]`, builds adapters and use cases, and registers routes. No code generation.
- **`cmd/<name>-service/main.go` is thin**: it calls `app.New()` then `Run()` and exits non-zero on error.

## Event Bus (NATS)

| Subject | Publisher | Consumer | Payload |
|---------|-----------|----------|---------|
| `user.created` | user-service | asset-service (durable consumer `asset-service-user-created` on stream `USERS`; creates the user's portfolio exactly once) | `{user_id, email, role}` |

## Caching Strategy (Redis)

- **user-service**: revoked access-token JTI set (TTL = token remaining life)
- **asset-service**: asset cache
- **price-service**: quote cache with configurable TTL

## Migrations

Services with a database embed their SQL in `internal/<svc>/migrations/` (`embed.FS`) and apply them at boot with `bootstrap.RunMigrations`.

## Key Source Files

| File | Purpose |
|------|---------|
| `go.mod` | The single module for all services |
| `internal/*/app/app.go` | Wiring per service |
| `cmd/*/main.go` | Service entrypoints |
| `config/<env>/*.yaml` | Per-environment configuration |
| `deploy/Dockerfile` | Shared service image (`SERVICE`, `PORT` args) |
