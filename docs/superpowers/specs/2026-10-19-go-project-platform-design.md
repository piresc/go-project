# go-project platform design

Agreed with the user on 2026-10-18/19. Goal: like parkir-pintar, a new feature in go-project should only need domain, use case, repository, handler and a few lines of wiring. Every infrastructure capability is wired once, switched on by config, and has a running example to copy.

Requires gokit **v0.5.0** published (trusted-proxy client IP, NATS trace propagation, DB/Redis spans, `OnShutdown` hooks).

## 1. Single module, parkir-pintar layout

One `go.mod` at the root, `module github.com/piresc/go-project`. `go.work` and the per-service modules go away.

```
cmd/<service>/main.go            thin: platform app New() → Run()
internal/<svc>/                  user, asset, price
  app/        wiring (short) + service-specific Config
  domain/ usecase/ repository/ gateway/ handler/http/
  migrations/ (embed.FS, services with a DB)
pkg/platform/                    the one place infrastructure is wired
config/<env>/<service>.yaml      local, production
deploy/                          one Dockerfile (ARG SERVICE), compose, Coolify, nginx
```

Binary and config names stay `user-service`, `asset-service`, `price-service`. Ports stay 8084 / 8082 / 8083, nginx 8080.

## 2. `pkg/platform`

`platform.New[S](name, opts...)` wraps `bootstrap.NewHTTPService`. `S` is the service's own config struct (`struct{}` if none). It returns `*platform.App[S]`, which embeds the bootstrap app (DB, Redis, NATS, Logger, Tracer, Mux, Config) and adds the following.

| Field / method | Built when | What it is |
|---|---|---|
| `Service S` | always | the service's own settings from YAML `service:` |
| `Middleware` | always | gokit middleware (for `RequireRole`) |
| `Auth`, `OptionalAuth` | `auth.public_key_*` set | RS256 middleware with Redis revocation, fail-closed (503) unless `service.auth.fail_open` |
| `Revocations` | Redis present | `auth.RedisRevocationStore` |
| `Events` | `nats.enabled` | JetStream: streams and consumers declared under `service.events`, JSON `Publish`, `Subscribe` with ack / retry / terminate |
| `Tasks` | `asynq.enabled` | asynq `Enqueue`, `Handle`, cron schedules from `service.tasks.schedules` |
| `Locker` | Redis present | gokit redislock, key prefix = service name |
| `HTTPClient(name)` | `service.http_clients.<name>` | gokit httpclient (retries, trace headers) behind a circuit breaker |

Options:
- `platform.WithMigrations(fs)` runs embedded migrations at startup.
- `platform.Require(...)` fails startup with a clear message when a needed dependency isn't configured.

`Run()` starts subscriptions, the task worker and the scheduler, then serves HTTP. On SIGINT/SIGTERM, HTTP drains first, then platform hooks stop consumers and workers through gokit `OnShutdown`, and only then do DB, Redis and NATS close.

Idempotency and retry are used straight from gokit inside use cases. They need no wiring.

## 3. Examples, one per capability

- **user-service** (existing): uses `Auth`. Publishes `user.created` through `Events`, with JetStream dedup on the user ID instead of core NATS.
- **asset-service**: consumes `user.created` and creates the user's portfolio exactly once (gokit `idempotency.Check` + `ON CONFLICT DO NOTHING`). `GET /portfolio` (Auth) returns it.
- **price-service**: a scheduled task `price:refresh` (every 15s locally, every minute in production) takes a lock so only one replica runs it. It fetches quotes through `HTTPClient("market")` and caches them in Redis. `GET /prices/top-performers` (OptionalAuth) serves the cached top 5. Locally the market is a static mock container.

## 4. Observability and edge

- Local compose adds `grafana/otel-lgtm`, one container with an OTel collector, Tempo, Loki, Prometheus and Grafana. Services export over OTLP gRPC (`tracing.exporter: otlp-grpc`, port 4317), so traces, metrics and logs share one endpoint. Grafana runs on :3000.
- Production config leaves tracing off. The Coolify env example gets an `OTEL_EXPORTER_OTLP_ENDPOINT` placeholder.
- nginx sends `X-Forwarded-For` so gokit's trusted-proxy client IP works. asset-service and price-service are enabled in nginx, both compose files and the e2e test.

## 5. How-to guide

`AGENTS.md` and `docs/ARCHITECTURE.md` gain an "Add a use case" recipe: which files to create, and the one wiring line per piece. That's what a future "add use case X to asset-service" request follows.

## Out of scope

Email, gRPC, a Go API gateway (nginx stays), Wire.
