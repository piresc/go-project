# go-project

A Go multi-service boilerplate built on [`github.com/piresc/gokit`](https://github.com/piresc/gokit). Each service is its own Go module under `services/`, joined by a root `go.work`. Services share one layout: thin `cmd/server/main.go`, hand wiring in `internal/app`, and clean layers beneath it (see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)).

## Services

| Service | Port | Database | Role |
|---------|------|----------|------|
| asset-service | 8082 | Postgres | Asset CRUD, consumes `user.created` |
| price-service | 8083 | none (Redis only) | Prices and top performers, guest-readable |
| user-service | 8084 | Postgres | Authentication and profiles (added later) |

## Start a new service

1. Copy `services/asset-service` (or `price-service` if it has no database) to `services/<name>`.
2. Change the module path in `go.mod` to `github.com/piresc/go-project/services/<name>` and fix imports.
3. Rename the service in `internal/app/app.go` and `config/*/<name>.yaml`, and set a free port.
4. Add `./services/<name>` to `go.work` and to `SERVICES` in the `Makefile`.
5. Run `go mod tidy` in the new module.

## Run locally

```bash
make keys
docker compose -f deploy/docker-compose.yml up
```

`make keys` writes an RSA key pair to `keys/` (git-ignored). The compose file arrives in a later task.

Useful targets: `make test`, `make vet`, `make lint`, `make build`.

## Deploying

The services are deployed with Coolify using a compose file (see `deploy/`, added later). Secrets such as the JWT public key are supplied through environment variables, never committed.
