# go-project

A Go multi-service boilerplate built on [`github.com/piresc/gokit`](https://github.com/piresc/gokit). All services live in one Go module (`github.com/piresc/go-project`). Services share one layout: a thin `cmd/<service>/main.go`, hand wiring in `internal/<svc>/app`, and clean layers beneath it (see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)).

## Services

| Service | Port | Schema | Redis DB | Role |
|---------|------|--------|----------|------|
| asset-service | 8082 | `assets` | 2 | Asset CRUD, consumes `user.created` |
| price-service | 8083 | none | 0 | Prices and top performers, guest-readable |
| user-service | 8084 | `users` | 1 | Authentication (Google/Apple OAuth, JWT, refresh rotation) and profiles |

One Postgres database (`app`) holds a schema and login role per service, and one Redis serves a DB number per service. A service never reads another's schema or Redis DB; Postgres enforces it through the per-service roles.

## Start a new service

1. Copy `internal/asset` (or `internal/price` if it has no database) to `internal/<svc>`, and `cmd/<same>-service` to `cmd/<name>-service`.
2. Fix the imports to `github.com/piresc/go-project/internal/<svc>/...`.
3. Rename the service in `internal/<svc>/app/app.go` and add `config/{local,production}/<name>-service.yaml` with a free port.
4. Add a service block to the compose files using `deploy/Dockerfile` with `SERVICE` and `PORT` build args. `make build` picks up `cmd/*` automatically.
5. If it has a database, add a role and schema to `deploy/bootstrap/init-db.sh`, run `make db-init` on an existing volume, then add the password env, `DB_*` env and `database:` / `redis.db` YAML (see "Add a service with a database" in docs/ARCHITECTURE.md). Pick a free Redis DB number.
6. Run `go mod tidy` at the repo root.

## Run locally

```bash
make keys    # RSA key pair in keys/ (git-ignored)
make up      # build and start nginx, the three services, market-mock, otel-lgtm, one postgres and one redis, nats; waits for health
make seed    # insert the test account (local only, never production)
make e2e     # run deploy/e2e_test.sh against http://localhost:8080 (E2E_DOCKER=1 enables the docker-based checks)
make down    # stop the stack and remove volumes
```

Run `make seed` before every `make e2e`: the e2e refresh rotates the seeded refresh token and logout revokes it, and the seed resets it. Optional overrides (`POSTGRES_PASSWORD`, `USER_DB_PASSWORD`, `ASSET_DB_PASSWORD`, TTLs, OAuth client IDs) go in `deploy/.env`, copied from `deploy/.env.example`. If port 8080 or 3000 is busy, set `NGINX_PORT` / `GRAFANA_PORT` and run e2e with a matching `BASE_URL`, for example `NGINX_PORT=18080 make up` then `BASE_URL=http://localhost:18080 make e2e`.

## Observability

The local stack runs `grafana/otel-lgtm` (Grafana, Tempo, Loki, Prometheus). Services export traces over OTLP gRPC to `otel-lgtm:4317`; production config has tracing disabled.

To see a trace:

1. Call an endpoint, for example `curl localhost:8080/api/v1/prices/top-performers`.
2. Open Grafana at http://localhost:3000 (anonymous admin in this image, or `admin` / `admin`).
3. Go to Explore, pick the Tempo data source, and search with the tag `service.name = price-service`. Open a trace to see its spans.

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
4. Set the environment variables from `deploy/runtime.coolify.env.example`. Required: `APP_DOMAIN`, `TRAEFIK_NETWORK` (the app network Traefik shares with the containers), `JWT_PRIVATE_KEY_PEM`, `JWT_PUBLIC_KEY_PEM`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `USER_DB_PASSWORD`, `ASSET_DB_PASSWORD`. Recommended: `GOOGLE_CLIENT_ID`, `APPLE_CLIENT_ID`. Required for price-service: `SERVICE_HTTP_CLIENTS_MARKET_BASE_URL` (upstream quotes API). Optional: `OTEL_EXPORTER_OTLP_ENDPOINT`, `APP_NAME` (prefix for Traefik router names and the postgres volume), `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`.
5. Map `main` to auto-deploy.
6. After deploying, check that an invalid `POST /api/v1/auth/google` returns `401`.

Notes:

- The Traefik routers deliberately have no redirect-to-https middleware: with Cloudflare in Flexible mode (HTTP to origin) a forced redirect loops forever. They use priority 9000 to win over Coolify's generated routers.
- One `postgres` container (database `app`, a schema and login role per service) and one `redis` container (DB per service). `POSTGRES_USER` / `POSTGRES_PASSWORD` is the admin login, used only by the init script and operators.
- The database volume is named `${APP_NAME:-go-project}-postgres-app-data`. Do not rename it unless you accept losing the data. The init script runs only on a fresh volume.
- To re-run the init script on a running deployment (for example after adding a service's role and schema), run `docker exec <postgres-container> bash /docker-entrypoint-initdb.d/00-init-db.sh`. It is idempotent and also updates the role passwords. Locally the equivalent is `make db-init`.
- Moving an existing Coolify deployment to this layout: stop the app, remove the old per-service postgres and redis containers and their volumes (there is no data to keep), then deploy. The old volumes are not reused.
- Never run `make seed` or the seed SQL against production.
