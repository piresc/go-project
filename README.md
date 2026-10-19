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
make up      # build and start nginx, the three services, market-mock, otel-lgtm, per-service postgres and redis, nats; waits for health
make seed    # insert the test account (local only, never production)
make e2e     # run deploy/e2e_test.sh against http://localhost:8080 (E2E_DOCKER=1 enables the docker-based checks)
make down    # stop the stack and remove volumes
```

Run `make seed` before every `make e2e`: the e2e refresh rotates the seeded refresh token and logout revokes it, and the seed resets it. Optional overrides (`POSTGRES_PASSWORD`, TTLs, OAuth client IDs) go in `deploy/.env`, copied from `deploy/.env.example`. If port 8080 or 3000 is busy, set `NGINX_PORT` / `GRAFANA_PORT` and run e2e with a matching `BASE_URL`, for example `NGINX_PORT=18080 make up` then `BASE_URL=http://localhost:18080 make e2e`.

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
4. Set the environment variables from `deploy/runtime.coolify.env.example`. Required: `APP_DOMAIN`, `TRAEFIK_NETWORK` (the app network Traefik shares with the containers), `JWT_PRIVATE_KEY_PEM`, `JWT_PUBLIC_KEY_PEM`, `POSTGRES_USER`, `POSTGRES_PASSWORD`. Recommended: `GOOGLE_CLIENT_ID`, `APPLE_CLIENT_ID`. Required for price-service: `SERVICE_HTTP_CLIENTS_MARKET_BASE_URL` (upstream quotes API). Optional: `OTEL_EXPORTER_OTLP_ENDPOINT`, `APP_NAME` (prefix for Traefik router names and the postgres volume), `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`.
5. Map `main` to auto-deploy.
6. After deploying, check that an invalid `POST /api/v1/auth/google` returns `401`.

Notes:

- The Traefik routers deliberately have no redirect-to-https middleware: with Cloudflare in Flexible mode (HTTP to origin) a forced redirect loops forever. They use priority 9000 to win over Coolify's generated routers.
- Each service owns its datastores: `postgres-user` / `redis-user` (user-service), `postgres-asset` / `redis-asset` (asset-service), `redis-price` (price-service). asset-service and price-service only read `redis-user`, to check token revocation.
- The user database volume is named `${APP_NAME:-go-project}-postgres-data` and the asset database volume `${APP_NAME:-go-project}-postgres-asset-data`. Do not rename them unless you accept losing the data. The two Postgres containers must never mount the same volume.
- Never run `make seed` or the seed SQL against production.

### Upgrading an existing Coolify deployment

Older deployments ran one `postgres` container (databases `users` and `assets`) and one `redis`. The new compose file splits them. `postgres-user` reuses the old `${APP_NAME:-go-project}-postgres-data` volume; `postgres-asset` starts empty and asset-service migrates it on boot. Move the asset data over like this:

1. Back up: run `pg_dumpall` from the old `postgres` container and store the dump off the server.
2. Stop the whole app in Coolify.
3. Confirm that no old `postgres` or `redis` container is still running (`docker ps`). If one is left orphaned, remove it. Never let it and `postgres-user` run on the same volume.
4. Deploy the new compose file. `postgres-user` comes up on the old volume; `postgres-asset` and the Redis containers start empty.
5. Stop asset-service. Copy the data with `pg_dump --data-only -t portfolios -t assets -d assets` run in `postgres-user`, piped into `psql -d assets` in `postgres-asset` (the schema already exists from migrations).
6. Start asset-service and check the data. Only then drop the `assets` database on `postgres-user`.

Token revocations stored in the old Redis are lost in the cutover, so tokens revoked before it work again until they expire (at most `ACCESS_TOKEN_TTL`).
