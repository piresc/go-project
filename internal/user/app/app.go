// Package app wires the user-service on top of pkg/platform.
package app

import (
	"context"
	"os"
	"time"

	"github.com/piresc/gokit/auth"

	"github.com/piresc/go-project/internal/user/gateway/cache"
	"github.com/piresc/go-project/internal/user/gateway/messaging"
	"github.com/piresc/go-project/internal/user/gateway/oauth"
	"github.com/piresc/go-project/internal/user/gateway/token"
	httptransport "github.com/piresc/go-project/internal/user/handler/http"
	"github.com/piresc/go-project/internal/user/migrations"
	"github.com/piresc/go-project/internal/user/repository/postgres"
	"github.com/piresc/go-project/internal/user/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

const (
	serviceName   = "user-service"
	warmCacheWait = 10 * time.Second
)

// App is the assembled user-service.
type App struct {
	app *platform.App[Config]
}

// New loads configuration, runs migrations and wires routes.
func New() (*App, error) {
	app, err := platform.New[Config](serviceName,
		platform.Require(platform.Postgres, platform.Redis, platform.Events, platform.AuthKeys),
		platform.WithMigrations(migrations.FS, "."),
	)
	if err != nil {
		return nil, err
	}

	cfg := app.Config
	priv, _, err := loadKeys(cfg.Auth)
	if err != nil {
		return nil, err
	}

	googleID := app.Service.GoogleClientID
	if v := os.Getenv("GOOGLE_CLIENT_ID"); v != "" {
		googleID = v
	}
	appleID := app.Service.AppleClientID
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

	repo := postgres.NewUserRepository(app.DB)
	userCache := cache.NewRedisCache(app.Redis)
	publisher := messaging.NewNATSPublisher(app.NATSClient)

	authUC := usecase.NewAuthUseCase(repo, userCache, app.Revocations, publisher, google, apple, tokens, cfg.Auth.AccessTTL, app.Logger)
	userUC := usecase.NewUserUseCase(repo, userCache, app.Logger)

	httptransport.RegisterRoutes(app.Mux, app.Auth,
		httptransport.NewAuthHandler(authUC, app.Logger),
		httptransport.NewUserHandler(userUC, userUC, app.Logger),
	)

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
