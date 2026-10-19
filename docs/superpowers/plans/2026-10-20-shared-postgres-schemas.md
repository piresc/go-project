# One Postgres with a schema per service, one Redis with a DB per service

Branch: `feat/shared-db-schemas` (from `main` at efcb38b).

## Goal

Make this layout go-project's default, because it's simpler on Coolify and AssetPulse will copy it:
- **Postgres:** one container and one database, with each service in its own schema.
- **Redis:** one container, with each service on its own DB number.

Isolation stays. Postgres enforces it through per-service users. Redis isolation is by DB number, which is a convention only.

## Decisions (user-approved)

**Postgres**
- One container (`postgres`, `postgres:15.14-bookworm`) and one database, `app` (`POSTGRES_DB`).
- One login user per service:

  | Service | User | Schema | Password env |
  |---|---|---|---|
  | user-service | `user_svc` | `users` | `USER_DB_PASSWORD` |
  | asset-service | `asset_svc` | `assets` | `ASSET_DB_PASSWORD` |

  price-service has no database.
- Each user owns its own schema, and its `search_path` is set to that schema. It gets no `USAGE` on any other schema, so a cross-schema read fails with "permission denied".
- SQL keeps unqualified table names. golang-migrate's `schema_migrations` table lands in each service's schema, through `current_schema()`.
- The admin `POSTGRES_USER`/`POSTGRES_PASSWORD` is used only by the init script and by operators. Services never use it.

**Redis**
- One container (`redis`).
- DB numbers: price-service `0` (asynq requires DB 0, which platform enforces), user-service `1`, asset-service `2`.
- No Redis ACLs, since gokit has no Redis username.

**Other**
- No gokit changes and no `pkg/platform` changes. Everything goes through config, deploy and docs.
- Production has no data, so there's no data migration in go-project. Volume names may change.

## Task 1: deploy, config, Makefile, e2e, docs (one task, since the parts are tightly coupled)

### Files

- `deploy/Dockerfile.postgres` (new): `postgres:15.14-bookworm`, copying `bootstrap/init-db.sh` to `/docker-entrypoint-initdb.d/00-init-db.sh`.
- `deploy/bootstrap/init-db.sh` (new):
  - Fail if `USER_DB_PASSWORD` or `ASSET_DB_PASSWORD` is empty.
  - Run against `$POSTGRES_DB` with `ON_ERROR_STOP` and `psql -v` variables, quoting the passwords safely (no shell interpolation into SQL):
    - `REVOKE ALL ON SCHEMA public FROM PUBLIC;`
    - `REVOKE CONNECT ON DATABASE <db> FROM PUBLIC;`
    - For each service:
      - `CREATE ROLE <user> LOGIN PASSWORD :'pw';`
      - `GRANT CONNECT ON DATABASE <db> TO <user>;`
      - `CREATE SCHEMA <schema> AUTHORIZATION <user>;`
      - `ALTER ROLE <user> IN DATABASE <db> SET search_path = <schema>;`
  - It runs only on a fresh volume. Write it so it's idempotent (`IF NOT EXISTS` / `DO` blocks), so `make db-init` can re-run it.
- `Makefile`:
  - `db-init` re-runs the init script in the running container. Document it for adding a new service to an existing volume.
  - `seed` runs the seed as `user_svc` (`psql -U user_svc -d app`, with the password from env), then the portfolio cleanup as `asset_svc`. As an alternative, run both as admin with an explicit `SET search_path`; pick whichever is simpler, but keep the SQL unqualified.
- `deploy/docker-compose.yml` and `deploy/docker-compose.coolify.yml`:
  - Replace `postgres-user` and `postgres-asset` with one `postgres` built from `Dockerfile.postgres`. It gets `POSTGRES_DB: app` and passes `USER_DB_PASSWORD`/`ASSET_DB_PASSWORD` to the init script.
  - Replace the three Redis containers with one `redis`.
  - Services:
    - **user-service:** `DB_HOST=postgres`, `DB_DATABASE=app`, `DB_USERNAME=user_svc`, `DB_PASSWORD=${USER_DB_PASSWORD}`, `REDIS_HOST=redis`.
    - **asset-service:** the same, with `asset_svc` and `ASSET_DB_PASSWORD`.
    - **price-service:** `REDIS_HOST=redis`.
  - Set the Redis DB numbers in the YAML; also set `REDIS_DB` in compose only if the env binding is verified to work.
  - Fix `depends_on`.
  - Local compose uses `${USER_DB_PASSWORD:-user-change-me}`-style defaults. Coolify uses `:?` guards.
  - **Volumes:**
    - Local: `postgres_data` plus the existing others.
    - Coolify: keep `${APP_NAME:-go-project}-postgres-data` and drop the asset volume. Keep the "do not rename" comments.
- `config/{local,production}/{user,asset,price}-service.yaml`: set `database.database: app`, `database.username` (`user_svc` / `asset_svc`), `database.host` (`postgres`, or `localhost` in local), `redis.host`, and `redis.db` (1 / 2 / 0).
- `deploy/.env.example` and `deploy/runtime.coolify.env.example`: add `USER_DB_PASSWORD` and `ASSET_DB_PASSWORD` as placeholders.
- `deploy/e2e_test.sh`: keep all existing checks.
  - **Redis isolation:** `redis-cli -n 2 EXISTS prices:quotes` prints `0`, and `-n 0` prints `1`. Run against the single `redis` container.
  - **Postgres isolation:** `psql -U asset_svc -d app -c 'SELECT 1 FROM users.users LIMIT 1'` fails with "permission denied". Also, `user_svc` sees its own table through `search_path`.
  - Any remaining reference to the removed containers is fixed.
- `README.md`, `docs/ARCHITECTURE.md`, `AGENTS.md`:
  - The service table gets Schema and Redis DB columns.
  - The rule becomes: a service never reads another service's schema or Redis DB; Postgres enforces this through per-service users.
  - New "Add a service with a database" steps: add a user and schema to `init-db.sh`, run `make db-init` on existing volumes, then add the env and YAML.
  - Remove the per-container wording.
  - Never name real domains or IDs.

### Plan-review rulings (binding; they override the text above where the two differ)

1. **New Postgres volume.**
   - Coolify uses the new volume `${APP_NAME:-go-project}-postgres-app-data`; local compose uses `postgres_app_data`. The old volume skips the init script because it's already initialised.
   - The README gets a short note on moving an existing Coolify deployment: stop the app, remove the old `postgres-user`, `postgres-asset` and `redis-*` containers and their volumes (no data to keep), then deploy.
2. **`init-db.sh` structure.**
   - Feed SQL to `psql -v ON_ERROR_STOP=1 -v user_pw="$USER_DB_PASSWORD" ...` on stdin through a quoted heredoc (`<<'EOSQL'`). Never use `-c` strings or `DO $$` blocks with `:'var'`, because psql doesn't substitute variables there.
   - Create each role with `SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', 'user_svc', :'user_pw') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'user_svc') \gexec`.
   - Then always run `ALTER ROLE user_svc PASSWORD :'user_pw';`, so a re-run updates the password.
   - Use `CREATE SCHEMA IF NOT EXISTS ... AUTHORIZATION ...`.
   - The database is `$POSTGRES_DB`. `make db-init` connects as admin to that database.
3. **One asynq service per Redis.** Document in ARCHITECTURE and AGENTS that at most one service may enable asynq per Redis. All asynq services share DB 0 and the default queue, so a second one would take price-service's tasks.
4. **Running the init script.**
   - Dockerfile: `COPY --chmod=0755 bootstrap/init-db.sh /docker-entrypoint-initdb.d/00-init-db.sh`. The Coolify build context is `./deploy`.
   - `make db-init` runs `docker compose exec -T postgres bash /docker-entrypoint-initdb.d/00-init-db.sh`, with the env already in the container.
   - The README shows the equivalent `docker exec <postgres-container> bash /docker-entrypoint-initdb.d/00-init-db.sh` for Coolify, which has no `make`.
5. **e2e log scan** covers only the service containers. The permission-denied check logs an ERROR on the Postgres side, by design.
6. **README Coolify env:** list `USER_DB_PASSWORD` and `ASSET_DB_PASSWORD` as required. The Postgres healthcheck is `pg_isready -h 127.0.0.1 -U $POSTGRES_USER -d app`.
7. **Local YAML passwords** match the local compose defaults.

### Acceptance

- `make vet test build` passes, and `gofmt` and lint are clean.
- Full stack on clean volumes (`down -v`, `make up`, `make seed`, e2e, then `make seed` and e2e again) passes both runs, with `NGINX_PORT=18080` and `BASE_URL=http://localhost:18080`. No ERROR or panic lines in the logs.
- The Coolify compose parses with dummy inline env.
- `git grep` finds no `postgres-user`, `postgres-asset`, `redis-user`, `redis-asset` or `redis-price` outside `docs/superpowers/`.
- No secrets committed.
