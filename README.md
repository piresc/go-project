# go-project

A Go multi-service boilerplate built on [`github.com/piresc/gokit`](https://github.com/piresc/gokit). All services live in one Go module (`github.com/piresc/go-project`). Services share one layout: a thin `cmd/<service>/main.go`, hand wiring in `internal/<svc>/app`, and clean layers beneath it (see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)).

## Services

| Service | Port | Database | Role |
|---------|------|----------|------|
| asset-service | 8082 | Postgres | Asset CRUD, consumes `user.created` |
| price-service | 8083 | none (Redis only) | Prices and top performers, guest-readable |
| user-service | 8084 | Postgres | Authentication (Google/Apple OAuth, JWT, refresh rotation) and profiles |

## Start a new service

1. Copy `internal/asset` (or `internal/price` if it has no database) to `internal/<svc>`, and `cmd/<same>-service` to `cmd/<name>-service`.
2. Fix the imports to `github.com/piresc/go-project/internal/<svc>/...`.
3. Rename the service in `internal/<svc>/app/app.go` and add `config/{local,production}/<name>-service.yaml` with a free port.
4. Add a service block to the compose files using `deploy/Dockerfile` with `SERVICE` and `PORT` build args. `make build` picks up `cmd/*` automatically.
5. Run `go mod tidy` at the repo root.

## Run locally

```bash
make keys    # RSA key pair in keys/ (git-ignored)
make up      # build and start nginx, user-service, postgres, redis, nats; waits for health
make seed    # insert the test account (local only, never production)
make e2e     # run deploy/e2e_test.sh against http://localhost:8080
make down    # stop the stack and remove volumes
```

Run `make seed` before every `make e2e`: the e2e refresh rotates the seeded refresh token and logout revokes it, and the seed resets it. Optional overrides (`POSTGRES_PASSWORD`, TTLs, OAuth client IDs) go in `deploy/.env`, copied from `deploy/.env.example`. asset-service and price-service blocks are commented in the compose files and in `deploy/nginx.conf`; uncomment them when those services are ready.

Useful targets: `make test`, `make vet`, `make lint`, `make build`.

## Key generation

```bash
make keys
```

This runs `openssl genrsa` and writes `keys/private.pem` and `keys/public.pem`. Locally they are mounted read-only into user-service at `/keys`. For Coolify, pass the PEM contents through `JWT_PRIVATE_KEY_PEM` and `JWT_PUBLIC_KEY_PEM` (multiline or `\n`-escaped). Never commit keys.

## Deploying with Coolify

The stack deploys as a single Coolify Docker Compose application using [`deploy/docker-compose.coolify.yml`](deploy/docker-compose.coolify.yml). Coolify runs it from the repo root, so build contexts are written relative to the root. Env template: [`deploy/runtime.coolify.env.example`](deploy/runtime.coolify.env.example).

1. Create a **Docker Compose** application in Coolify and point it at this repository.
2. Set the compose path to `deploy/docker-compose.coolify.yml`.
3. Expose `nginx` on port `8080`.
4. Set the environment variables from `deploy/runtime.coolify.env.example`. Required: `APP_DOMAIN`, `TRAEFIK_NETWORK` (the app network Traefik shares with the containers), `JWT_PRIVATE_KEY_PEM`, `JWT_PUBLIC_KEY_PEM`, `POSTGRES_USER`, `POSTGRES_PASSWORD`. Recommended: `GOOGLE_CLIENT_ID`, `APPLE_CLIENT_ID`. Optional: `APP_NAME` (prefix for Traefik router names and the postgres volume), `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`.
5. Map `main` to auto-deploy.
6. After deploying, check that an invalid `POST /api/v1/auth/google` returns `401`.

Notes:

- The Traefik routers deliberately have no redirect-to-https middleware: with Cloudflare in Flexible mode (HTTP to origin) a forced redirect loops forever. They use priority 9000 to win over Coolify's generated routers.
- The postgres volume is named `${APP_NAME:-go-project}-postgres-data`. Do not rename it unless you accept losing the data.
- Never run `make seed` or the seed SQL against production.
