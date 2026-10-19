# Service-to-service calls and per-service datastores

Branch: `feat/service-calls` (from `main` at 9f94b35).

## Goal

1. One service calls another over HTTP. asset-service values a user's portfolio by asking price-service for quotes.
2. Each service owns its own Postgres and Redis. No two services share a database container or a Redis.

## Decisions (orchestrator rulings; the user skipped questions)

- **Transport: HTTP**, through the existing `platform.HTTPClient` (gokit httpclient, 3 attempts, gobreaker). No gRPC, no protobuf.
- **Calls go container to container**, e.g. `http://price-service:8083`, not through nginx.
- **No auth on the internal call.** `GET /prices/quotes` uses `OptionalAuth`, like `/prices/top-performers`: prices are public data. gokit's `httpclient.Client` can't set custom headers, so forwarding a user's token would need platform work that's out of scope here.
- **Holdings model:** a holding is `(user_id, symbol, quantity)`. Quantity is a positive `float64`/`DOUBLE PRECISION`, the same float type as `Quote.Price`.
- **Datastores:** `postgres-user` (db `users`), `postgres-asset` (db `assets`), `redis-user`, `redis-asset`, `redis-price`. NATS stays shared because it's the bus.
- **Credentials:** both Postgres containers keep using the shared `POSTGRES_USER`/`POSTGRES_PASSWORD` env vars, so existing deployment env keeps working.
- **Production data:** in Coolify, `postgres-user` keeps the existing volume `${APP_NAME:-go-project}-postgres-data`, so user data survives. `postgres-asset` gets the new volume `${APP_NAME:-go-project}-postgres-asset-data` and starts empty. A one-off copy of the old `assets` database is documented, not automated.
- **The one shared Redis read: token revocation.** user-service writes revoked token IDs to its own Redis, and every service has to see them. asset-service and price-service therefore read revocations from `redis-user` through a dedicated, revocation-only client. Their own Redis stays theirs for everything else. Replacing this with revocation events is a possible follow-up.
- **Seeded and pre-existing users:** `SetHolding` creates the portfolio idempotently, so users who never got a `user.created` event can still add holdings.

## Global constraints

- Same as the platform plan: never copy `assetpulse-app/keys/`, `.env`, real domains or Traefik network IDs. No keys are committed. Secret checks use `git grep`. Never add a `replace` for gokit.
- AGENTS.md rules: slog only; `response.Success`/`response.Error`; infrastructure only through `platform.App`; no Wire, no chi.
- Layering: usecases depend on ports (interfaces in `usecase/ports.go`). Adapters live in `repository/` and `gateway/`. Handlers depend on small interfaces.
- Done means `make vet test build` passes, `gofmt -l .` is empty, and `golangci-lint run ./...` is clean.

---

## Task 0 — platform: gokit envelope decoding and a revocation Redis

**Files:** `pkg/platform/httpclient.go` + test, `pkg/platform/auth.go`, `pkg/platform/config.go`, `pkg/platform/platform.go` (close the extra client on stop), plus tests.

- **Envelope decoding.** `decodeJSONBody` must also unwrap gokit's real envelope, `{"status":"success","data":...}` and `{"status":"error","error":"..."}`, which is what `response.Success`/`response.Error` write (gokit v0.5.1 `response/response.go`). `status` other than `"success"` is an error carrying the `error` text. Keep the existing `success`-key handling and plain JSON working.
  - Add a test that decodes a body produced by calling `response.Success` on an `httptest.ResponseRecorder`, and one for `response.Error`.
- **Revocation Redis.** Add `Settings.Auth.RevocationRedis`, under `service.auth.revocation_redis`, with `host`, `port`, `password` and `db`.
  - When `host` is set, `initAuth` builds a separate go-redis client for it (use gokit's redis package if it has a constructor from host/port/password/db; otherwise go-redis directly, matching how platform builds others). It pings the client at startup and fails with a clear error if the ping fails.
  - It uses that client for `Revocations` only; `a.Redis` is untouched.
  - The client is closed during infra close, after the hooks-done flag, following the existing stop order.
  - When `host` is empty, behavior is unchanged.
  - Env override: check whether `SERVICE_AUTH_REVOCATION_REDIS_HOST` works through the existing env binding, the same way `SERVICE_HTTP_CLIENTS_MARKET_BASE_URL` does. If it does, note it in the report. If not, make it work.
  - Tests use miniredis:
    - With the setting, a revocation written to the revocation Redis is rejected by `Auth`, while the same JTI written to `a.Redis` is not.
    - Without the setting, the old behavior holds.
- `TestShutdown_StopOrder` must still pass unchanged.

**Acceptance:** new tests pass; `make vet test build` green.

## Task 1 — price-service: `GET /prices/quotes`

**Files:** `internal/price/usecase/quotes.go` (new) + test, `internal/price/handler/http/price_handler.go`, `internal/price/handler/http/router.go`, handler test, `internal/price/app/app.go`.

- `QuotesUsecase{cache QuoteCache}`, built with `NewQuotesUsecase(cache)`.
- `Get(ctx, symbols []string) ([]domain.Quote, error)` returns the cached quotes whose symbol is in `symbols`, in the order of `symbols`. Unknown symbols are left out, duplicates are collapsed, and the result is a non-nil slice. It reads only the cache, never the market.
- Route `GET /prices/quotes?symbols=AAPL,NVDA`, wrapped in `OptionalAuth`.
- Handler parsing: split on `,`, trim, upper-case, drop empties.
  - No symbols after parsing → 400 `"symbols is required"`.
  - More than 50 symbols → 400 `"too many symbols"`.
  - Any symbol not matching `^[A-Z0-9.\-]{1,10}$` → 400 `"invalid symbol"`.
  - Cache error → 500, logged at error like the other handlers.
- Response: `response.Success(200, []quoteResponse)`, using the same `quoteResponse` as top-performers.

**Acceptance:** usecase tests (order, unknown, duplicates, empty cache) and handler tests (200, each 400 case, 500) pass.

## Task 2 — asset-service: holdings and portfolio value via price-service

**Files:**
- `internal/asset/migrations/000003_holdings.{up,down}.sql`
- `internal/asset/domain/portfolio.go`
- `internal/asset/usecase/{ports.go,portfolio.go,errors.go}` + tests
- `internal/asset/repository/postgres/portfolio_repository.go` + test
- `internal/asset/gateway/price/price.go` (new) + test
- `internal/asset/handler/http/{portfolio_handler.go,router.go}` + test
- `internal/asset/handler/event/user_created_test.go` (its stub repo and the `NewPortfolioUsecase` call must follow the new signatures)
- `internal/asset/app/{app.go,config.go}`
- `config/{local,production}/asset-service.yaml`

**Steps:**

- **Migration 000003:**
  ```sql
  CREATE TABLE IF NOT EXISTS holdings (user_id UUID NOT NULL REFERENCES portfolios(user_id) ON DELETE CASCADE, symbol TEXT NOT NULL, quantity DOUBLE PRECISION NOT NULL CHECK (quantity > 0), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (user_id, symbol));
  ```
  The down migration is `DROP TABLE IF EXISTS holdings;`.
- **Domain:**
  - `Holding{Symbol string; Quantity float64}` with db tags.
  - `Valuation{Holdings []HoldingValue; Total float64; Unpriced []string}`.
  - `HoldingValue{Symbol; Quantity; Price; Value}`.
  - `Quote{Symbol; Price}` is asset-service's own view of a quote. Don't import price-service packages.
- **Ports:**
  - `PortfolioRepository` gains `UpsertHolding(ctx, userID, symbol, qty) error`, `DeleteHolding(ctx, userID, symbol) error` (returns ErrNotFound when the row is missing) and `ListHoldings(ctx, userID) ([]domain.Holding, error)`.
  - New `PriceSource` interface: `Quotes(ctx, symbols []string) ([]domain.Quote, error)`.
  - New error `ErrPricesUnavailable`.
- **Usecase** (`NewPortfolioUsecase(repo, prices)`):
  - `SetHolding(ctx, userID, symbol, qty)`:
    - Validate the UUID.
    - Upper-case the symbol and check it against the same regex as Task 1.
    - `0 < qty <= 1e12`, finite, not NaN; otherwise `ErrInvalidInput`.
    - Create the portfolio first with the idempotent `repo.Create`, so seeded and pre-existing users work.
    - At most 50 holdings per user (the price-service batch limit). Adding a 51st new symbol is rejected with a new `ErrTooManyHoldings`, mapped to 409 `"too many holdings"`. Updating an existing symbol is always allowed.
  - `RemoveHolding`.
  - `Get`, which now also returns holdings. Either change its return to a struct, or add `Holdings(ctx, userID)` and leave `Get`'s return unchanged. Keep `GET /portfolio` backward compatible: its existing fields stay, and `holdings` is added.
  - `Value(ctx, userID)`:
    - Portfolio missing → `ErrNotFound`.
    - No holdings → zero Valuation without calling prices.
    - Otherwise call `PriceSource.Quotes` once with all symbols. `Value = qty*price`, `Total` is the sum, and symbols with no quote go to `Unpriced` (sorted).
    - Any PriceSource error → wrap it as `ErrPricesUnavailable`.
- **Gateway** `gateway/price`:
  - `New(c JSONGetter)`, where `JSONGetter` is a narrow `GetJSON` interface. Follow `internal/price/gateway/market/market.go` and its test stub; `*platform.HTTPClient` satisfies it in `app.go`.
  - It calls `GET /prices/quotes?symbols=<url-escaped comma list>` and decodes the response into `[]domain.Quote`.
  - The gateway test feeds the stub a body produced by `response.Success`. That decoding is done by the platform client (Task 0), so the gateway test checks the endpoint string and result mapping.
  - Add an httptest round-trip test in the gateway or the app package. It uses a real `platform` client if one can be built through exported API; otherwise skip it and rely on e2e.
- **HTTP routes,** all behind `Auth`:
  - `PUT /portfolio/holdings/{symbol}` with body `{"quantity": number}` → 200 with the holding. Invalid → 400. No portfolio → 404.
  - `DELETE /portfolio/holdings/{symbol}` → 204 (or 200 with `response.Success` if gokit has no 204 helper; follow existing style). Missing → 404.
  - `GET /portfolio` adds a `holdings` array.
  - `GET /portfolio/value`:
    - Returns 200 with `{holdings:[{symbol,quantity,price,value}], total, unpriced:[]}`.
    - `ErrPricesUnavailable` → 503 `"prices unavailable"`, logged at warn.
    - Missing portfolio → 404.
- **Config:**
  - Add `service.http_clients.price` with `base_url: http://price-service:8083` and `timeout: 1s` in both local and production asset-service yaml. 3 attempts × 1s stays under nginx's 5s `proxy_read_timeout`, so an outage gives 503, not 504. Production can override it with `SERVICE_HTTP_CLIENTS_PRICE_BASE_URL`; check that the existing env binding works for this key, as it does for `market`.
  - Remove the dead `MarketBaseURL` field from `asset/app/config.go`.
- **App wiring:** `app.HTTPClient("price")` → gateway → usecase.

**Acceptance:**
- Usecase tests with fakes: valuation math, unpriced, empty holdings make no call, price error → ErrPricesUnavailable, and validation.
- Gateway test against httptest, covering the gokit envelope and escaping.
- Handler tests for each status code.
- Repository tests for upsert, delete-missing → ErrNotFound and cascade, run the same way the existing repo tests run (Docker).

## Task 3 — deploy: per-service Postgres and Redis, e2e

**Files:** `deploy/docker-compose.yml`, `deploy/docker-compose.coolify.yml`, `config/production/*.yaml`, `config/local/*.yaml` (only if a host default changes), `deploy/Dockerfile.postgres`, `deploy/bootstrap/init-db.sh`, `Makefile` (`seed`), `deploy/e2e_test.sh`, `deploy/.env.example` (if needed), `deploy/runtime.coolify.env.example`, `README.md`.

**Compose (local and Coolify):**
- Replace `postgres` with `postgres-user` and `postgres-asset`.
  - Each is `postgres:15.14-bookworm` with `POSTGRES_DB` set (`users` / `assets`), and keeps the existing healthcheck form.
  - Delete `Dockerfile.postgres` and `init-db.sh`, since `POSTGRES_DB` replaces them. Keep `seed-test-account.sql`.
- Replace `redis` with `redis-user`, `redis-asset` and `redis-price`, all `redis:7.4.8-alpine` with the existing healthcheck.
- Point each service's `DB_HOST`/`REDIS_HOST` at its own container, and fix its `depends_on`.
- asset-service gets `depends_on: price-service: service_healthy`.
- **Coolify:**
  - `postgres-user` keeps the volume `${APP_NAME:-go-project}-postgres-data`, with the existing "Do not rename" comment.
  - `postgres-asset` uses the new `${APP_NAME:-go-project}-postgres-asset-data`.
  - Local compose: `postgres_user_data` and `postgres_asset_data`.
- Production yaml defaults: `database.host: postgres-user` / `postgres-asset`, and `redis.host: redis-user` / `redis-asset` / `redis-price`.
- `make seed` runs exec against `postgres-user`. It then also runs, against `postgres-asset` (db `assets`), `DELETE FROM portfolios WHERE user_id = '<seeded user id from seed-test-account.sql>'`, so the existing "no portfolio → 404" e2e check passes on every rerun. The cascade removes the holdings too.
- **Revocation Redis wiring:**
  - asset-service and price-service get `REVOCATION_REDIS_HOST=redis-user` and `REVOCATION_REDIS_PORT=6379`, or `SERVICE_AUTH_REVOCATION_REDIS_*` if that's what Task 0 established.
  - In the yaml, `service.auth.revocation_redis.host: redis-user` is set for both local and production configs of asset and price.
  - asset and price get `depends_on: redis-user: service_healthy`.

**README / ARCHITECTURE: "Upgrading an existing Coolify deployment" runbook.** The two Postgres containers must never mount the same volume, and asset-service migrates `postgres-asset` on boot. The steps:
1. Back up: `pg_dumpall` from the old `postgres` container.
2. Stop the whole app in Coolify.
3. Confirm that no old `postgres` or `redis` container is still running. If one is left orphaned, remove it. Never let it and `postgres-user` run on the same volume.
4. Deploy the new compose. `postgres-user` reuses the old volume; `postgres-asset` and the Redis containers start empty.
5. Stop asset-service. Copy the data with `pg_dump --data-only -t portfolios -t assets -d assets` from `postgres-user`, piped into `psql -d assets` on `postgres-asset` (the schema already exists from migrations).
6. Start asset-service. Only after you've checked the data, drop the `assets` db on `postgres-user`.

Note in the runbook: token revocations stored in the old Redis are lost, so tokens revoked before the cutover work again until they expire (at most `ACCESS_TOKEN_TTL`).

**e2e additions (keep all 27 existing checks):**
- `GET /api/v1/prices/quotes?symbols=NVDA,AAPL` returns 200 with 2 quotes in that order.
- `GET /api/v1/prices/quotes` returns 400.
- PUT holding NVDA qty 2 returns 200.
- PUT holding with qty 0 returns 400.
- `GET /portfolio` includes NVDA.
- `GET /portfolio/value` returns 200, and its total equals 2 × the NVDA price returned by quotes (compare with `jq` or `awk`, whichever the script already uses).
- PUT holding `ZZZZ` returns 200; then `/portfolio/value` lists `ZZZZ` in `unpriced`.
- DELETE `ZZZZ` returns 204/200; DELETE it again returns 404.
- Isolation check: `docker compose -f deploy/docker-compose.yml exec -T redis-asset redis-cli EXISTS prices:quotes` prints `0`, while `redis-price` prints `1`.
- Breaker check at the end of the script, before logout:
  - `docker compose -f deploy/docker-compose.yml stop price-service`, then `/portfolio/value` returns 503.
  - `docker compose -f deploy/docker-compose.yml up -d --wait price-service`.
- After the existing logout check: `GET /api/v1/portfolio` with the logged-out token returns 401. This proves cross-service revocation works with split Redis.
  
  If the script is meant to also run against a remote BASE_URL, guard the docker-dependent checks behind `E2E_DOCKER=1` and set it in the local make target or document it.

**Acceptance:** `make up` works on clean volumes (`docker compose down -v` first); e2e passes twice in a row with `NGINX_PORT=18080 BASE_URL=http://localhost:18080`; `git grep` secret check is clean.

## Task 4 — docs

**Files:** `docs/ARCHITECTURE.md`, `AGENTS.md`.

- **Overview:** services own their datastores; they talk via NATS events (async) and HTTP (sync reads).
- **Service Boundaries:** the table gets a Database and a Redis column per service.
- **New section "Service calls (HTTP)":**
  - Calls use `platform.HTTPClient` from `service.http_clients.<name>`. The caller owns a `gateway/<callee>` adapter behind a usecase port, with its own domain types.
  - The breaker turns upstream outages into `ErrUnavailable`, which the usecase maps to a domain error and the handler maps to 503.
  - The internal call is unauthenticated, so the callee must expose only public data on such routes.
  - Table of current calls: asset → price `GET /prices/quotes`.
- **"Add a use case"** recipe gets a step: needs another service's data → add a gateway plus an http_clients entry; never read its DB or Redis.
- **AGENTS.md rule:** a service never connects to another service's database or Redis. Cross-service data goes through events or HTTP. The one exception is the platform's read-only token-revocation client on `redis-user`; document why.

**Acceptance:** the docs match the code; no stale "share infrastructure" wording remains.
