# AGENTS.md

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before changing code. It defines the service layout, layering rules and event table.

Quick rules:
- One Go module per service under `services/`, tied together by the root `go.work`.
- Build on `github.com/piresc/gokit` (bootstrap, middleware, response, auth). No Google Wire, no chi.
- Log with the `slog` logger from bootstrap, never `log.Printf`.
- Respond with `response.Success` / `response.Error`.
- Verify with `make vet test build` before finishing.
