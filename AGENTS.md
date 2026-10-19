# AGENTS.md

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before changing code. It defines the service layout, layering rules, platform capabilities and event table. To add a feature, follow its "Add a use case" recipe.

Quick rules:
- One Go module at the repo root (`github.com/piresc/go-project`). Services are `cmd/<service>/main.go` plus `internal/<svc>/`.
- Infrastructure is wired only in `pkg/platform`; services use `platform.App` and never open DB, Redis, NATS or asynq themselves.
- A service never connects to another service's database or Redis. Cross-service data goes through NATS events or HTTP (a `gateway/<callee>` adapter on `platform.HTTPClient`, see "Service calls (HTTP)"). The one exception is the platform's read-only token-revocation client on `redis-user` (`service.auth.revocation_redis`): user-service writes revoked token IDs to its own Redis and every service must see them.
- Build on `github.com/piresc/gokit` (bootstrap, middleware, response, auth). No Google Wire, no chi.
- Log with the `slog` logger from bootstrap, never `log.Printf`.
- Respond with `response.Success` / `response.Error`.
- Verify with `make vet test build` before finishing.
