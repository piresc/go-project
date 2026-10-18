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
