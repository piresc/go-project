# Architecture

## Overview

go-project is a set of independent Go services built on `github.com/piresc/gokit`. Services talk to each other through NATS for async events and share infrastructure (PostgreSQL, Redis).

## Service Boundaries

| Service | Responsibility | Port | Database |
|---------|---------------|------|----------|
| **user-service** | Authentication, user profiles, token management | 8084 | Postgres |
| **asset-service** | Asset CRUD | 8082 | Postgres |
| **price-service** | Prices, top performers | 8083 | None (Redis only) |

Each service is an independent Go module (`services/<name>/go.mod`) with its own `internal/` tree. The root `go.work` ties the modules together for local development.

## Auth

Tokens are RS256 JWTs. user-service signs them with the private key; every other service only holds the public key and verifies with `auth.NewRSAVerifier`. Services install `middleware.RS256Auth` (or `OptionalRS256Auth` for guest-readable routes) with a `RedisRevocationStore`, so revoked token IDs are rejected. There is no shared auth package inside this repo: gokit provides it.

## Internal Layering (per service)

```
cmd/server/     main.go: thin entrypoint
internal/
  app/          Config, hand wiring on top of gokit bootstrap, lifecycle
  domain/       Core entities, value objects, validation
  usecase/      Business logic, port interfaces, typed errors
  repository/   Postgres adapters implementing repository ports
  gateway/      Redis, NATS, market-data adapters
  handler/http/ HTTP transport: routes, DTOs, error mapping
```

Design rules:
- **Handlers are transport-only**: no business logic in the HTTP layer.
- **Ports live in `usecase/ports.go`**: use cases depend on interfaces defined there.
- **Adapters implement ports**: repositories and gateways are easy to substitute in tests.
- **Hand wiring in `internal/app` on top of gokit bootstrap**: `New()` calls `bootstrap.NewHTTPService[Config]`, builds adapters and use cases, and registers routes. No code generation.
- **`cmd/server/main.go` is thin**: it calls `app.New()` then `Run()` and exits non-zero on error.

## Event Bus (NATS)

| Subject | Publisher | Consumer | Payload |
|---------|-----------|----------|---------|
| `user.created` | user-service | asset-service | `{user_id, email, role}` |

## Caching Strategy (Redis)

- **user-service**: revoked access-token JTI set (TTL = token remaining life)
- **asset-service**: asset cache
- **price-service**: quote cache with configurable TTL

## Migrations

Services with a database embed their SQL in `migrations/` (`embed.FS`) and apply them at boot with `bootstrap.RunMigrations`.

## Key Source Files

| File | Purpose |
|------|---------|
| `go.work` | Workspace of all service modules |
| `services/*/internal/app/app.go` | Wiring per service |
| `services/*/cmd/server/main.go` | Service entrypoints |
| `services/*/config/<env>/*.yaml` | Per-environment configuration |
