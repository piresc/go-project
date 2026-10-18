# go-project Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn go-project into a single-module template where every infrastructure capability is wired once in `pkg/platform`, and each capability has a running example.

**Architecture:** `pkg/platform` wraps gokit `bootstrap.NewHTTPService`, adding Auth, JetStream Events, asynq Tasks with cron, a Locker and named HTTP clients with a circuit breaker. Each service's `internal/<svc>/app` only builds its own adapters, use cases and handlers. Lifecycle goes through gokit `OnShutdown`, so workers stop before connections close.

**Tech Stack:** Go 1.26, gokit v0.5.1, sony/gobreaker/v2, nats.go jetstream, hibiken/asynq (scheduler), sqlx, go-redis v9, nats-server/v2 (tests), testcontainers (tests), grafana/otel-lgtm (local only).

**Spec:** `docs/superpowers/specs/2026-10-19-go-project-platform-design.md`

## Global Constraints

- **Precondition:** gokit **v0.5.1** (v0.5.0 plus `OnShutdown`) is on proxy.golang.org. Check with `curl -s -o /dev/null -w '%{http_code}' https://proxy.golang.org/github.com/piresc/gokit/@v/v0.5.1.info` → `200`. Never add a `replace` to a local gokit.
- Branch `feat/platform` from main `5412842` plus this plan's commit. Implementers never run git.
- Module `github.com/piresc/go-project`. Service names `user-service`, `asset-service`, `price-service`. Internal dirs `internal/user`, `internal/asset`, `internal/price`. Ports 8084 / 8082 / 8083, nginx 8080.
- Never copy `assetpulse-app/keys/`, `.env`, real domains (`app.example.com`), Traefik network IDs (`<traefik-network-id>`) or router names. No keys committed.
- Every task ends with `go build ./... && go vet ./... && golangci-lint run ./... && go test -race ./...` clean from the repo root. Container tests skip when Docker is unavailable.
- Tasks that touch deploy files also run `make up && make seed && make e2e` → all assertions pass, and pass again on a second `make seed && make e2e`. The refresh step revokes the seeded token, so always re-seed before e2e. Finish with `make down`.
- Local compose sets `APP_ENV: local` on every service. The image default stays `production`, so Coolify uses the production YAML.
- Secret checks use `git grep`, never plain `grep`, because `make keys` creates `keys/private.pem` in the working tree.
- gokit's worker and client always use Redis DB 0. `config/*/*.yaml` keep `redis.db: 0`, and platform rejects `redis.db != 0` when `asynq.enabled`.
- Keep layering from `docs/ARCHITECTURE.md`: ports in `usecase/ports.go`, adapters implement them, handlers are transport-only, responses via `response.Success`/`response.Error`, logging via the bootstrap `slog` logger.
- `pkg/platform` imports only gokit and third-party packages, never `internal/...`.

## Review Focus

1. A redelivered `user.created` (same user ID twice, or a crash before ack) must create exactly one portfolio. Task 4 test `TestEnsurePortfolio_Redelivered`.
2. Three price-service replicas firing the same cron tick must fetch from the market once. Task 5 test `TestRefresh_LockHeldSkips`.
3. A market outage must not hang requests or the worker. After the breaker opens, refresh fails fast and `GET /prices/top-performers` keeps serving the last cached data. Task 5 tests `TestRefresh_BreakerOpenFailsFast` and `TestTopPerformers_ServesStaleCache`.
4. A handler error that will never succeed (malformed JSON event) must not be redelivered forever. Task 3 test `TestSubscribe_PermanentErrorTerminates`.
5. SIGTERM while a consumer handler is running must let the handler finish before NATS and DB close. Task 2 test `TestRun_ShutdownOrder` and Task 3 test `TestSubscribe_DrainsOnShutdown`.

---

### Task 1: Single module, parkir-pintar layout (no behaviour change)

**Files:**
- Create: `go.mod`, `go.sum` (root), `cmd/{user-service,asset-service,price-service}/main.go`, `deploy/Dockerfile` (`ARG SERVICE`, `ARG PORT`), `config/{local,production}/<service>.yaml`
- Move: `services/user-service/internal/*` → `internal/user/*`, and likewise for `asset` and `price`. `services/<svc>/migrations` → `internal/<svc>/migrations`. Rewrite every import to `github.com/piresc/go-project/internal/<svc>/...`.
- Delete: `go.work`, `go.work.sum`, `services/` (all per-service `go.mod`/`go.sum`/`Dockerfile`/`.dockerignore`)
- Modify: `Makefile` (single-module `test`/`vet`/`lint`; `build` loops `cmd/*` into `bin/`), `deploy/docker-compose.yml` and `deploy/docker-compose.coolify.yml` (`context: ..` and `.` respectively, `dockerfile: deploy/Dockerfile`, `args: {SERVICE: user-service, PORT: 8084}`), `.gitignore`, `.dockerignore` (root, keeps `.env`, `*.pem`, `keys/`, `docs/` out), `docs/ARCHITECTURE.md`, `AGENTS.md`, `README.md`
- Bump `github.com/piresc/gokit` to `v0.5.1`.
- Update key paths: `../../keys` becomes `keys/` in local YAML, because the repo root is now the working directory.
- Root `.dockerignore` uses `**/.env`, `**/*.pem` and `keys/`, so `COPY config/` can't bring in `config/.env`.

**Interfaces:**
- Produces: the layout in spec §1. `deploy/Dockerfile` details:
  - Declares `ARG SERVICE` and `ARG PORT` after each `FROM`.
  - Builds `./cmd/${SERVICE}` to the fixed path `/app/server`.
  - Copies `config/` to `/app/config/`.
  - Sets `ENV APP_ENV=production` and `EXPOSE ${PORT}`.
  - Local compose overrides `APP_ENV: local`. gokit reads `config/<APP_ENV>/<name>.yaml` relative to the working directory, so `make` targets and tests run from the repo root.

- [ ] **Step 1:** Do the moves and import rewrites, and create the root module.
- [ ] **Step 2:** Global check → clean. The test count equals the old per-module total (13 test files, same test names).
- [ ] **Step 3:** `make up && make seed && make e2e`, then `make seed && make e2e` → both pass. `make down`.
- [ ] **Step 4:** `grep -rn 'services/' --include='*.go' --include='*.yml' --include=Makefile .` finds no stale paths, except ones inside `docs/superpowers/`.

### Task 2: `pkg/platform` core: App, options, Auth, lifecycle

**Files:**
- Create: `pkg/platform/{platform.go,config.go,options.go,auth.go}` with tests `platform_test.go`, `auth_test.go`
- Modify: `internal/{user,asset,price}/app/app.go` and `config.go` (adopt platform), `cmd/*/main.go`

**Interfaces:**
- Consumes: gokit `bootstrap.NewHTTPService`, `HTTPApp.OnShutdown`, `auth.NewRSAVerifier`, `auth.LoadRSAPublicKey`, `auth.NewRedisRevocationStore`, `middleware.RS256Options`.
- Produces:
  ```go
  type App[S any] struct {
      *bootstrap.HTTPApp[serviceConfig[S]]
      Service      S
      Settings     Settings
      Middleware   *middleware.Middleware
      Auth         func(http.Handler) http.Handler // nil when no public key configured
      OptionalAuth func(http.Handler) http.Handler // nil when no public key configured
      Revocations  *auth.RedisRevocationStore      // nil without Redis
      // Task 3 adds: Events *Events (nil unless nats.enabled)
      // Task 5 adds: Tasks *Tasks (nil unless asynq.enabled), Locker *redislock.Locker (nil without Redis)
  }
  func New[S any](name string, opts ...Option) (*App[S], error)
  func (a *App[S]) Run() error
  type Option func(*options)
  func WithMigrations(fsys embed.FS, dir string) Option // gokit RunMigrations takes embed.FS
  func Require(deps ...Dependency) Option
  type Dependency int // Postgres, Redis, Events, Tasks, AuthKeys
  ```
- `serviceConfig[S]` is unexported: `struct{ Settings `mapstructure:",squash"`; App S `mapstructure:",squash"` }`, read from YAML `service:`. `S` must be a struct; document `struct{}` for none.
- `Settings` (YAML under `service:`): `Auth struct{ FailOpen bool `mapstructure:"fail_open"` }`, `Events EventsSettings` (Task 3), `Tasks TasksSettings` (Task 5), `HTTPClients map[string]HTTPClientSettings `mapstructure:"http_clients"`` (Task 5), `Lock LockSettings` (Task 5). Tasks 3 and 5 add their fields to `Settings`; this task defines only `Auth`.
- `Require` error text: `<name> requires <dep>: <hint>`. Hints: Postgres `set database.host`, Redis `set redis.host`, Events `set nats.enabled and nats.url`, Tasks `set asynq.enabled`, AuthKeys `set auth.public_key_path or JWT_PUBLIC_KEY_PEM`.
- When a public key is configured, Redis is required (revocation), otherwise `New` fails. `Auth` uses `RS256Auth` and `OptionalAuth` uses `OptionalRS256Auth`, both with `RS256Options{Revocation: Revocations, FailOpen: Settings.Auth.FailOpen}`.
- `Run()` runs registered start funcs in order (Tasks 3 and 5 register theirs). If one fails, it calls `Shutdown` and returns the error before serving. Otherwise it calls `HTTPApp.Run()`. If that returns an error (e.g. the port is in use), `Run` also calls `Shutdown`, because gokit skips it on that path, and returns both errors joined.
- Stop funcs are registered with `OnShutdown` at `New` time. Hooks run newest-first, so register consumers first, then the task worker, then the scheduler. On shutdown the scheduler stops enqueueing first, then the worker finishes, then consumers drain.

- [ ] **Step 1: Write failing tests** (`platform_test.go`, with config files written to `t.TempDir()` and `t.Chdir` into it):
  - `TestNew_RequireMissing`: `Require(Postgres)` with no `database.host` → error equals `svc requires postgres: set database.host`.
  - `TestNew_ServiceConfigDecoded`: YAML `service: {greeting: hi}` with `S = struct{ Greeting string `mapstructure:"greeting"` }` → `a.Service.Greeting == "hi"`.
  - `TestNew_AuthNilWithoutKey`: no key → `a.Auth == nil`.
  - `TestNew_AuthNeedsRedis`: key configured, no Redis → error containing `redis`.
  - `TestRun_StartErrorStopsBeforeServe`: a registered start func returning `errBoom` → `Run()` returns `errBoom`, and stop funcs ran.
  - `TestRun_ShutdownOrder` (miniredis): a stop func pings Redis and records the result. Call `a.Shutdown(ctx)` directly; don't send signals. The ping succeeds, which proves the hook ran before Redis closed.
  - `auth_test.go` `TestAuth_RejectsRevoked`: miniredis plus a `testkit.NewRSAKeyPair` key. A token whose JTI is revoked → 401 `token revoked`. Redis closed → 503 with `Retry-After: 5`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** Run → PASS.
- [ ] **Step 5:** Port the three services to `platform.New`:
  - user-service uses `Require(Postgres, Redis, Events, AuthKeys)` and `WithMigrations`. It keeps its signer and `loadKeys` for the private key, and the existing `keys_test.go` still passes.
  - asset-service uses `Require(Postgres, Redis, AuthKeys)` (Task 4 adds `Events`). price-service uses `Require(Redis, AuthKeys)`. Without these, a nil `a.Auth` would panic in the router.
  - asset-service and price-service use `a.Auth` / `a.OptionalAuth`.
  - Each `internal/<svc>/app/app.go` contains no infrastructure construction (no `bootstrap.`, `RS256Options` or `LoadRSAPublicKey`). Check with `grep -n 'bootstrap\.\|RS256Options\|LoadRSAPublicKey' internal/*/app/app.go` → none.
- [ ] **Step 6:** Global check, then e2e with a re-seed each run → pass.

### Task 3: Events (JetStream) and user-service publishing

**Files:**
- Create: `pkg/platform/events.go`, `pkg/platform/events_test.go`
- Modify: `pkg/platform/config.go` (`EventsSettings`), `internal/user/gateway/messaging/nats.go` (uses `*platform.Events`), `config/*/user-service.yaml` (stream `USERS`), `deploy/docker-compose*.yml`: change `nats` to:
  - image `nats:2.12.0-alpine`
  - `command: ["-js", "-sd", "/data", "-m", "8222"]`
  - named volume `nats_data`, with the name pinned in the Coolify file
  - healthcheck `wget -qO- http://localhost:8222/healthz?js-enabled-only=true`
  - services depend on it with `condition: service_healthy`

**Interfaces:**
- Produces:
  ```go
  type EventsSettings struct {
      Streams   []StreamSettings   `mapstructure:"streams"`
      Consumers []ConsumerSettings `mapstructure:"consumers"`
  }
  type StreamSettings struct { Name string; Subjects []string; MaxAge time.Duration `mapstructure:"max_age"` } // limits retention, file storage; MaxAge default 168h
  type ConsumerSettings struct { Name, Stream, Subject string; MaxDeliver int `mapstructure:"max_deliver"`; AckWait time.Duration `mapstructure:"ack_wait"` } // defaults 5 and 30s
  type Event struct { Subject string; ID string; Data []byte } // ID = Nats-Msg-Id header
  var ErrPermanent = errors.New("permanent event failure")
  func (e *Events) Publish(ctx context.Context, subject string, v any, msgID string) error // JSON body
  func (e *Events) Subscribe(consumer string, h func(ctx context.Context, ev Event) error) error // before Run
  ```
- At `New`: create or update every declared stream and durable consumer (explicit ack, deliver all). gokit `CreateStream` is already CreateOrUpdate. A service that publishes to or consumes from a stream declares that stream identically.
- `Subscribe` on an undeclared consumer → error `consumer <name> not declared in service.events.consumers`. Subscribing twice to the same consumer → error.
- On start, each subscription runs gokit `ConsumeContext`, so the handler's ctx carries the trace. Handler results:
  - `nil` → `Ack`.
  - `errors.Is(err, ErrPermanent)` → `Term`, logged at error level.
  - any other error → `NakWithDelay(5s)`, logged at warn level with the delivery count.
- On stop, `Drain()` each consume context and wait for in-flight handlers, bounded by the shutdown ctx.
- user-service publishes `user.created` with `msgID = user.ID`.

- [ ] **Step 1: Write failing tests** against an in-process JetStream server (`nats-server/v2/test`, `-js`, `StoreDir: t.TempDir()`), one per test:
  - `TestPublishSubscribe_RoundTrip`: publish `{ "a": 1 }` on `t.x` and the handler gets the same JSON and `ID`.
  - `TestPublish_DedupByMsgID`: two publishes with the same msgID produce one delivery (stream `Duplicates` default window).
  - `TestSubscribe_RetryThenAck`: the handler fails once with a plain error, then succeeds, so it's called twice. Use a short `NakWithDelay` set through an unexported test hook.
  - `TestSubscribe_PermanentErrorTerminates`: the handler returns `fmt.Errorf("bad: %w", ErrPermanent)`. With the Nak delay hook set to 10ms, it's still called exactly once after 1s, so a plain Nak would make this test fail.
  - `TestSubscribe_UndeclaredConsumer`: the error text from Interfaces.
  - `TestSubscribe_DrainsOnShutdown`: the handler blocks 300ms. Calling the stop func waits for it to return before returning.
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.
- [ ] **Step 5:** user-service publisher test: an in-process server and a consumer on `user.created` receive `{user_id, email, role}` with `ID == user_id`.
- [ ] **Step 6:** Global check, then e2e with a re-seed each run → pass.

### Task 4: asset-service consumes `user.created` → portfolio

**Files:**
- Create: `internal/asset/migrations/000002_portfolios.{up,down}.sql`, `internal/asset/domain/portfolio.go`, `internal/asset/usecase/portfolio.go` and its test, `internal/asset/repository/postgres/portfolio_repository.go` and its test, `internal/asset/handler/event/user_created.go` and its test, `internal/asset/handler/http/portfolio_handler.go`
- Modify: `internal/asset/usecase/ports.go`, `internal/asset/handler/http/router.go`, `internal/asset/app/app.go`, `config/*/asset-service.yaml` (stream `USERS` identical to user-service's; consumer `asset-service-user-created` on `user.created`), `docs/ARCHITECTURE.md` (event table)

**Interfaces:**
- Table `portfolios(user_id UUID PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`.
- `type PortfolioRepository interface { Get(ctx, userID string) (*domain.Portfolio, error); Create(ctx, userID string) error }`. `Get` returns `usecase.ErrNotFound` when there's no row. `Create` uses `INSERT ... ON CONFLICT (user_id) DO NOTHING`.
- `func (u *PortfolioUsecase) EnsurePortfolio(ctx, userID string) error`: validates the UUID (an invalid one returns `ErrInvalidInput`), then `idempotency.Check(ctx, userID, repo.Get, ErrNotFound, "ensure portfolio")`. It returns early when the portfolio already exists, otherwise calls `Create`.
- `func (u *PortfolioUsecase) Get(ctx, userID string) (*domain.Portfolio, error)`
- The event handler decodes `{user_id}`. A decode error or `ErrInvalidInput` is wrapped with `platform.ErrPermanent`. Other errors are returned as-is, so the event is retried.
- `GET /portfolio` (Auth) → 200 `{user_id, created_at}`, or 404 `portfolio not found`.

- [ ] **Step 1: Write failing tests:**
  - Use case with a fake repo: `TestEnsurePortfolio_CreatesOnce`, `TestEnsurePortfolio_Redelivered` (two calls make one `Create` call), `TestEnsurePortfolio_InvalidID`.
  - Handler: `TestUserCreated_MalformedIsPermanent`, `TestUserCreated_RepoErrorRetries` (not `ErrPermanent`).
  - Repository (testcontainers Postgres plus migrations): `TestPortfolioRepo_CreateTwiceOneRow`.
- [ ] **Step 2:** FAIL. **Step 3:** Implement and wire (and add `Events` to asset-service's `Require`): `a.Events.Subscribe("asset-service-user-created", eventhandler.NewUserCreated(uc).Handle)`. **Step 4:** PASS.
- [ ] **Step 5:** Global check → clean.

### Task 5: Tasks, Locker, HTTPClient; price-service refresh

**Files:**
- Create: `pkg/platform/{tasks.go,httpclient.go,lock.go}` with tests, `internal/price/usecase/refresh.go` and `top.go` with tests, `internal/price/gateway/market/market.go` (replace stub) with its test, `internal/price/gateway/cache/redis.go` (replace stub) with its test, `internal/price/handler/task/refresh.go`, `deploy/market-mock/{Dockerfile,quotes.json,nginx.conf}` (7 quotes; this task owns market-mock, and Task 6 only wires it into compose)
- Modify: `pkg/platform/config.go`, `internal/price/{domain,usecase/ports.go,handler/http,app}`, `config/*/price-service.yaml`, `deploy/docker-compose*.yml` (asynq on Redis; `market-mock` in local compose only)

**Interfaces:**
- Platform:
  ```go
  type TasksSettings struct { Schedules []ScheduleSettings `mapstructure:"schedules"` }
  type ScheduleSettings struct { Task string `mapstructure:"task"`; Cron string `mapstructure:"cron"` }
  func (t *Tasks) Handle(taskType string, h func(ctx context.Context, payload []byte) error) // before Run
  func (t *Tasks) Enqueue(ctx context.Context, taskType string, payload any, delay time.Duration) (string, error)
  type LockSettings struct { TTL time.Duration `mapstructure:"ttl"` } // default 30s; prefix = service name; RetryAttempts 0
  type HTTPClientSettings struct {
      BaseURL string `mapstructure:"base_url"`; Timeout time.Duration `mapstructure:"timeout"` // default 5s
      FailureThreshold uint32 `mapstructure:"failure_threshold"`; OpenTimeout time.Duration `mapstructure:"open_timeout"` // 5, 30s
  }
  type HTTPClient struct { /* gokit httpclient + sony/gobreaker/v2 */ }
  func (c *HTTPClient) GetJSON(ctx context.Context, endpoint string, out any) error
  var ErrUnavailable = errors.New("upstream unavailable") // returned (wrapped) while the breaker is open
  ```
- Breaker: use `sony/gobreaker/v2` directly, not gokit `circuitbreaker`. gokit's fixes `Interval` at 60s, so 5 failures in a row never happen at 1–4 calls a minute. Settings:
  - `Interval: 0`, so counts never reset while closed.
  - `Timeout: OpenTimeout`.
  - `ReadyToTrip: ConsecutiveFailures >= FailureThreshold`.
  - `IsSuccessful`: true unless the error is a transport error, a timeout or an HTTP 5xx. A 4xx doesn't count against the breaker.
- gokit's httpclient makes 3 attempts per call and returns untyped errors for non-2xx responses. The breaker therefore wraps one `GetJSON` call, which may be 3 attempts, and classifies the status by parsing the error, or by using `Do` and checking `resp.StatusCode`. Prefer `Do`.
- The worker uses gokit `asynq.NewServer(addr, password, cfg.Asynq.Concurrency or 5)`. The scheduler uses `asynq.NewScheduler(asynq.RedisClientOpt{Addr, Password}, nil)`. Each schedule is registered with `asynq.Unique(<schedule interval>)` and `asynq.MaxRetry(0)`, so N replicas enqueue one task per tick and a failed refresh waits for the next tick instead of retrying. At `Run`, a schedule naming an unhandled task fails startup with `schedule <task> has no handler`. Both stop via `OnShutdown`.
- `HTTPClient(name)` with no `service.http_clients.<name>` returns the error `http client <name> not configured`. Clients are built in `New`, with `Tracer: a.Tracer`.
- price-service:
  - `domain.Quote{Symbol string; Price float64; ChangePct float64}`.
  - Ports: `QuoteSource.Quotes(ctx) ([]domain.Quote, error)`, `QuoteCache.Set(ctx, []domain.Quote) error` and `Get(ctx) ([]domain.Quote, error)` (Redis key `prices:quotes`, no TTL so stale data survives an outage), `Locker.Acquire(ctx, key)`, satisfied by `*redislock.Locker` via a small adapter.
  - `RefreshUsecase.Refresh(ctx) error`: acquire `refresh`. If the lock is held (`redislock.ErrLockUnavailable`), log at debug level and return nil. Otherwise fetch, cache, release. The lock protects against overlapping runs. The unique task stops duplicates per tick.
  - `TopUsecase.Top(ctx, n int) ([]domain.Quote, error)`: sort by `ChangePct` descending and take `n`. An empty cache returns an empty slice.
  - `GET /prices/top-performers` (OptionalAuth) → 200 with the top 5.
  - Market adapter: `GetJSON(ctx, "/v1/quotes", &resp)`, with wire DTO `struct{ Quotes []struct{ Symbol string `json:"symbol"`; Price float64 `json:"price"`; ChangePct float64 `json:"change_pct"` } `json:"quotes"` }`. The HTTP response uses the same JSON tags (`symbol`, `price`, `change_pct`).
  - Config: `service.tasks.schedules: [{task: price:refresh, cron: "@every 15s"}]` locally and `"@every 1m"` in production. `service.http_clients.market.base_url`: `http://market-mock:8080` locally; in production the YAML has `base_url: ""`. The key must be present, because viper's AutomaticEnv only overrides keys it already knows. The env var `SERVICE_HTTP_CLIENTS_MARKET_BASE_URL` overrides it. `New` fails with `http client market: base_url is empty` when it's still empty.

- [ ] **Step 1: Write failing tests:**
  - Platform:
    - `TestHTTPClient_BreakerOpens`: an httptest server returning 500, with threshold 2. After 2 failed calls the server has seen `2 × 3` requests (gokit's 3 attempts each). A third call returns `ErrUnavailable` and the counter doesn't move.
    - `TestHTTPClient_4xxDoesNotTrip`: a server returning 404 for 5 calls leaves the breaker closed.
    - `TestHTTPClient_EmptyBaseURL`: the startup error from Interfaces.
    - `TestTasks_UniquePerTick`: enqueueing the same scheduled task twice within the unique window yields one task (testcontainers Redis).
    - `TestNew_RejectsRedisDBWithAsynq`: `redis.db: 1` with `asynq.enabled` → error.
    - `TestHTTPClient_NotConfigured`.
    - `TestTasks_ScheduleWithoutHandler`: `Run` returns the error from Interfaces.
    - `TestTasks_EnqueueHandled`: miniredis isn't enough for asynq, so use testcontainers Redis and skip without Docker. The handler receives the payload.
  - price:
    - `TestRefresh_LockHeldSkips`: the fake locker returns `ErrLockUnavailable`, so the source isn't called and the result is nil.
    - `TestRefresh_BreakerOpenFailsFast`: the source returns `ErrUnavailable`, and `Refresh` returns it without caching.
    - `TestTopPerformers_ServesStaleCache`: the cache holds 7 quotes and the source is down, so `Top(5)` returns the 5 highest `ChangePct`, in order.
    - `TestTop_EmptyCache`: returns `[]`, not nil.
- [ ] **Step 2:** FAIL. **Step 3:** Implement and wire:
  ```go
  a.Tasks.Handle("price:refresh", taskhandler.Refresh(refreshUC))
  market.New(must(a.HTTPClient("market")))
  ```
  **Step 4:** PASS.
- [ ] **Step 5:** Global check → clean.

### Task 6: Observability, edge and deploy

**Files:**
- Modify:
  - `deploy/docker-compose.yml`: add `otel-lgtm` (pinned `grafana/otel-lgtm` tag at least two weeks old, ports `3000:3000`, internal 4317). Enable asset-service and price-service with healthchecks and `depends_on`, plus `market-mock`.
  - `deploy/docker-compose.coolify.yml`: enable asset-service and price-service. Don't add otel-lgtm or market-mock. price-service `environment:` passes `SERVICE_HTTP_CLIENTS_MARKET_BASE_URL` and `OTEL_EXPORTER_OTLP_ENDPOINT` from the Coolify env. All services get `stop_grace_period: 35s` (gokit shutdown timeout 30s plus margin). Local compose: every service sets `APP_ENV: local`.
  - `deploy/nginx.conf`: uncomment the assets block, and add `location /api/v1/portfolio` → asset-service and `/api/v1/prices` → price-service. Every location gets `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`.
  - `config/local/*.yaml`: `tracing: {enabled: true, exporter: otlp-grpc, otlp_endpoint: otel-lgtm:4317, sample_rate: 1.0}`.
  - `config/production/*.yaml`: tracing disabled.
  - `deploy/runtime.coolify.env.example`: `OTEL_EXPORTER_OTLP_ENDPOINT=`, `SERVICE_HTTP_CLIENTS_MARKET_BASE_URL=`.
  - `deploy/bootstrap/init-db.sh`: keep `users assets`.
  - `deploy/e2e_test.sh`.
  - `README.md`: Grafana URL, and how to see a trace.

**e2e additions** (must pass twice in a row):
- `GET /api/v1/portfolio` without a token → 401.
- With the seeded token → 404 `portfolio not found`. The seed bypasses `user.created`, which is expected; the consumer is covered by Task 3/4 tests.
- `GET /api/v1/prices/top-performers` without a token → 200. Within 30s, poll until `.data | length == 5`, and assert the first item has the highest `change_pct` in `deploy/market-mock/quotes.json`.
- `GET /api/v1/assets` with a token → 200.

- New authenticated e2e checks go before the existing logout step (`e2e_test.sh` around line 113), because logout revokes the token.

- [ ] **Step 1:** Make the deploy changes. **Step 2:** `make up && make seed && make e2e`, then `make seed && make e2e` → both pass.
- [ ] **Step 3:** Trace check:
  1. Run `curl -s localhost:8080/api/v1/prices/top-performers`.
  2. Query Tempo through Grafana's API (`curl -s 'localhost:3000/api/datasources/proxy/uid/tempo/api/search?tags=service.name%3Dprice-service'`) and get at least one trace.
  3. If the datasource UID differs in the pinned image, report the working query instead of failing.
- [ ] **Step 4:** Secret check with `git grep -nE 'assetpulse\.piresc\.dev|<traefik-network-id>|-----BEGIN [A-Z ]*PRIVATE KEY-----' -- ':!docs/superpowers'` → no matches. Also check `git ls-files | grep -E '\.pem$|(^|/)\.env$'` → nothing. `make down`.

### Task 7: "Add a use case" guide and final verification

**Files:**
- Modify: `AGENTS.md`, `docs/ARCHITECTURE.md`

- [ ] **Step 1:** In `docs/ARCHITECTURE.md`:
  - Replace the per-module text with the single-module layout.
  - Add a "Platform capabilities" table (spec §2) with each field's YAML switch.
  - Add an "Add a use case" recipe:
    1. domain type
    2. port in `usecase/ports.go`
    3. use case plus test with fakes
    4. adapter in `repository/` or `gateway/`, plus migration
    5. handler: HTTP route, event `Subscribe`, or task `Handle`
    6. one wiring line in `internal/<svc>/app/app.go`
    7. config keys

  Each step points to the file in the examples that does it, e.g. Task 4's portfolio for events and Task 5's refresh for tasks, lock and HTTP client.
- [ ] **Step 2:** `AGENTS.md`: replace the "One Go module per service" rule with the single-module rule. Point to the recipe. Add "infrastructure is wired only in `pkg/platform`".
- [ ] **Step 3 (orchestrator):**
  - Combined-diff review by `code-reviewer`, while `implementer` runs the global check plus `make up && make seed && make e2e` and then `make seed && make e2e`.
  - Then merge `feat/platform` into main locally. go-project has no remote; publishing is the user's call.
