// Package app wires the asset-service on top of gokit bootstrap.
package app

import (
	"errors"
	"fmt"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/bootstrap"
	"github.com/piresc/gokit/middleware"

	httptransport "github.com/piresc/go-project/services/asset-service/internal/handler/http"
	"github.com/piresc/go-project/services/asset-service/internal/repository/postgres"
	"github.com/piresc/go-project/services/asset-service/internal/usecase"
	"github.com/piresc/go-project/services/asset-service/migrations"
)

const serviceName = "asset-service"

// App is the assembled asset-service.
type App struct {
	app *bootstrap.HTTPApp[Config]
}

// New loads configuration, runs migrations and wires routes.
func New() (*App, error) {
	app, err := bootstrap.NewHTTPService[Config](serviceName)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if app.Redis == nil {
		return nil, errors.New("redis is required for token revocation checks")
	}
	if app.DB == nil {
		return nil, errors.New("database is required")
	}

	if err := bootstrap.RunMigrations(migrations.FS, ".", app.Config.Database); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	pub, err := auth.LoadRSAPublicKey(app.Config.Auth.PublicKeyPEM, app.Config.Auth.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load public key: %w", err)
	}

	mw := middleware.NewMiddleware(app.Logger, app.Tracer)
	authMW := mw.RS256Auth(auth.NewRSAVerifier(pub), middleware.RS256Options{
		Revocation: auth.NewRedisRevocationStore(app.Redis),
	})

	repo := postgres.NewAssetRepository(app.DB)
	uc := usecase.NewAssetUsecase(repo)
	httptransport.RegisterRoutes(app.Mux, httptransport.Deps{
		Auth:   authMW,
		Assets: httptransport.NewAssetHandler(uc),
	})

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
