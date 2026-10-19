// Package platform builds a service on top of gokit bootstrap: it loads
// config, checks required infrastructure, wires auth and runs a managed
// lifecycle. Services own their domain code; platform owns the plumbing.
//
// platform imports only gokit and third-party packages, never internal/....
package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/bootstrap"
	"github.com/piresc/gokit/middleware"
	gokitredis "github.com/piresc/gokit/redis"
	"github.com/piresc/gokit/redislock"
)

const defaultShutdownTimeout = 30 * time.Second

// App is a service assembled by New. The embedded HTTPApp exposes the
// infrastructure gokit connected (DB, Redis, NATSClient, Logger, Mux, ...).
type App[S any] struct {
	*bootstrap.HTTPApp[serviceConfig[S]]

	// Service is the service's own config, read from the "service:" YAML key.
	Service S
	// Settings are the platform settings, read from the same key.
	Settings Settings
	// Middleware is the shared gokit middleware set.
	Middleware *middleware.Middleware
	// Auth requires a valid token. Nil when no public key is configured.
	Auth func(http.Handler) http.Handler
	// OptionalAuth accepts requests with or without a token. Nil when no
	// public key is configured.
	OptionalAuth func(http.Handler) http.Handler
	// Revocations checks and records revoked token IDs. Nil without Redis.
	Revocations *auth.RedisRevocationStore
	// Events publishes and consumes JetStream events. Nil without NATS.
	Events *Events
	// Tasks enqueues background tasks and runs the worker and scheduler. Nil
	// unless asynq is enabled.
	Tasks *Tasks
	// Locker takes distributed locks, prefixed with the service name. Nil
	// without Redis.
	Locker *redislock.Locker

	httpClients map[string]*HTTPClient

	// revocationRedis is the dedicated revocation client, nil when unset.
	revocationRedis *gokitredis.Client

	stopTrace func(name string) // tests only, see options.stopTrace

	mu      sync.Mutex
	starts  []func(context.Context) error
	stopped atomic.Bool // set by the first shutdown hook to run (the last of them)
}

// New loads <name>'s config, connects the configured infrastructure, verifies
// the Require'd dependencies, runs migrations and builds the auth middleware.
//
// S is the service's own config struct (read from "service:"). Use struct{}
// when the service has none. When a public key is configured Redis is required,
// so revoked tokens can be rejected.
func New[S any](name string, opts ...Option) (*App[S], error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	hub, err := bootstrap.NewHTTPService[serviceConfig[S]](name)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	a := &App[S]{
		HTTPApp:    hub,
		Service:    hub.Config.Service.App,
		Settings:   hub.Config.Service.Settings,
		Middleware: middleware.NewMiddleware(hub.Logger, hub.Tracer),
		stopTrace:  o.stopTrace,
	}
	// Registered first, so it runs last of the hooks: it only records that a
	// shutdown happened, so Run does not shut down twice.
	a.onStopNamed("hooks-done", func(context.Context) error { a.stopped.Store(true); return nil })

	// Hooks run newest first. Registered right after hooks-done, this one runs
	// after every capability hook and just before hooks-done, which is last.
	a.onStop(func(context.Context) error {
		if a.revocationRedis == nil {
			return nil
		}
		return a.revocationRedis.Close()
	})

	if err := a.setup(name, &o); err != nil {
		if serr := a.Shutdown(context.Background()); serr != nil {
			a.Logger.Warn("cleanup after failed init", "error", serr)
		}
		return nil, err
	}
	return a, nil
}

func (a *App[S]) setup(name string, o *options) error {
	for _, dep := range o.require {
		if !a.has(dep) {
			return fmt.Errorf("%s requires %s: %s", name, dep, dep.hint())
		}
	}
	if o.migrations != nil {
		if !a.has(Postgres) {
			return fmt.Errorf("%s requires %s: %s (needed for migrations)", name, Postgres, Postgres.hint())
		}
		if err := bootstrap.RunMigrations(o.migrations.fsys, o.migrations.dir, a.Config.Database); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}
	if err := a.initAuth(name); err != nil {
		return err
	}
	if err := a.initEvents(); err != nil {
		return err
	}
	if err := a.initTasks(); err != nil {
		return err
	}
	if err := a.initLocker(name); err != nil {
		return err
	}
	return a.initHTTPClients()
}

// initEvents builds Events when NATS is connected and ties its lifecycle to
// the app. Consumers stop last of the platform hooks, after later workers.
func (a *App[S]) initEvents() error {
	if a.NATSClient == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()
	ev, err := NewEvents(ctx, a.NATSClient, a.Logger, a.Settings.Events)
	if err != nil {
		return fmt.Errorf("events: %w", err)
	}
	a.Events = ev
	a.onStopNamed("events", ev.stop)
	a.onStart(ev.start)
	return nil
}

// has reports whether the dependency was configured and connected.
func (a *App[S]) has(d Dependency) bool {
	switch d {
	case Postgres:
		return a.DB != nil
	case Redis:
		return a.Redis != nil
	case NATS:
		return a.NATSClient != nil
	case Asynq:
		return a.AsynqClient != nil
	case AuthKeys:
		return a.keyConfigured()
	default:
		return false
	}
}

// onStart registers fn to run, in registration order, at the start of Run
// before the server accepts traffic.
func (a *App[S]) onStart(fn func(context.Context) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.starts = append(a.starts, fn)
}

// onStop registers fn to run on shutdown before infrastructure is closed.
// Stop funcs run newest-first, so register what must stop last first.
func (a *App[S]) onStop(fn func(context.Context) error) {
	a.OnShutdown(fn)
}

// onStopNamed is onStop for a platform hook, reporting name to stopTrace
// (when set) just before fn runs.
func (a *App[S]) onStopNamed(name string, fn func(context.Context) error) {
	a.onStop(func(ctx context.Context) error {
		if a.stopTrace != nil {
			a.stopTrace(name)
		}
		return fn(ctx)
	})
}

// Run runs the registered start funcs in order, then serves HTTP until a
// termination signal. A failing start func shuts the app down and its error
// is returned before serving. If serving fails (for example the port is in
// use) gokit skips shutdown, so Run does it and returns both errors joined.
func (a *App[S]) Run() error {
	a.mu.Lock()
	starts := append([]func(context.Context) error(nil), a.starts...)
	a.mu.Unlock()

	for _, fn := range starts {
		if err := fn(context.Background()); err != nil {
			return a.failShutdown(err)
		}
	}
	if err := a.HTTPApp.Run(); err != nil {
		return a.failShutdown(err)
	}
	return nil
}

// failShutdown shuts the app down after err and returns err joined with any
// shutdown error.
func (a *App[S]) failShutdown(err error) error {
	if a.stopped.Load() {
		// gokit already ran Shutdown (the error came from the shutdown itself).
		return err
	}
	timeout := defaultShutdownTimeout
	if t := a.Config.Server.ShutdownTimeout; t > 0 {
		timeout = time.Duration(t) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if serr := a.Shutdown(ctx); serr != nil {
		return errors.Join(err, serr)
	}
	return err
}
