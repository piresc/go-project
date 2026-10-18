// Package app wires the user-service on top of gokit bootstrap.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/bootstrap"
	"github.com/piresc/gokit/middleware"

	"github.com/piresc/go-project/internal/user/gateway/cache"
	"github.com/piresc/go-project/internal/user/gateway/messaging"
	"github.com/piresc/go-project/internal/user/gateway/oauth"
	"github.com/piresc/go-project/internal/user/gateway/token"
	httptransport "github.com/piresc/go-project/internal/user/handler/http"
	"github.com/piresc/go-project/internal/user/migrations"
	"github.com/piresc/go-project/internal/user/repository/postgres"
	"github.com/piresc/go-project/internal/user/usecase"
)

const (
	serviceName   = "user-service"
	warmCacheWait = 10 * time.Second
)

// App is the assembled user-service.
type App struct {
	app *bootstrap.HTTPApp[Config]
}

// New loads configuration, runs migrations and wires routes.
func New() (*App, error) {
	app, err := bootstrap.NewHTTPService[Config](serviceName)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if app.DB == nil {
		return nil, errors.New("database is required")
	}
	if app.Redis == nil {
		return nil, errors.New("redis is required for caching and token revocation")
	}
	if app.NATSClient == nil {
		return nil, errors.New("nats is required (set nats.enabled: true) to publish user events")
	}

	cfg := app.Config
	priv, pub, err := loadKeys(cfg.Auth)
	if err != nil {
		return nil, err
	}

	if err := bootstrap.RunMigrations(migrations.FS, ".", cfg.Database); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	googleID := cfg.Service.GoogleClientID
	if v := os.Getenv("GOOGLE_CLIENT_ID"); v != "" {
		googleID = v
	}
	appleID := cfg.Service.AppleClientID
	if v := os.Getenv("APPLE_CLIENT_ID"); v != "" {
		appleID = v
	}
	google := oauth.NewGoogleVerifier(googleID)
	apple := oauth.NewAppleVerifier(appleID)

	warmCtx, cancel := context.WithTimeout(context.Background(), warmCacheWait)
	defer cancel()
	if err := google.WarmCache(warmCtx); err != nil {
		app.Logger.Warn("google JWKS warm-up failed", "error", err)
	}
	if err := apple.WarmCache(warmCtx); err != nil {
		app.Logger.Warn("apple JWKS warm-up failed", "error", err)
	}

	issuer := cfg.Auth.Issuer
	if issuer == "" {
		issuer = serviceName
	}
	signer := auth.NewRSASigner(priv, issuer, cfg.Auth.AccessTTL)
	tokens := token.NewService(signer, cfg.Auth.RefreshTTL)
	revocations := auth.NewRedisRevocationStore(app.Redis)

	repo := postgres.NewUserRepository(app.DB)
	userCache := cache.NewRedisCache(app.Redis)
	publisher := messaging.NewNATSPublisher(app.NATSClient)

	authUC := usecase.NewAuthUseCase(repo, userCache, revocations, publisher, google, apple, tokens, cfg.Auth.AccessTTL, app.Logger)
	userUC := usecase.NewUserUseCase(repo, userCache, app.Logger)

	mw := middleware.NewMiddleware(app.Logger, app.Tracer)
	authMW := mw.RS256Auth(auth.NewRSAVerifier(pub), middleware.RS256Options{Revocation: revocations})

	httptransport.RegisterRoutes(app.Mux, authMW,
		httptransport.NewAuthHandler(authUC, app.Logger),
		httptransport.NewUserHandler(userUC, userUC, app.Logger),
	)

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
