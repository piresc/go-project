// Package app wires the asset-service on top of pkg/platform.
package app

import (
	httptransport "github.com/piresc/go-project/internal/asset/handler/http"
	"github.com/piresc/go-project/internal/asset/migrations"
	"github.com/piresc/go-project/internal/asset/repository/postgres"
	"github.com/piresc/go-project/internal/asset/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

const serviceName = "asset-service"

// App is the assembled asset-service.
type App struct {
	app *platform.App[Config]
}

// New loads configuration, runs migrations and wires routes.
func New() (*App, error) {
	app, err := platform.New[Config](serviceName,
		platform.Require(platform.Postgres, platform.Redis, platform.AuthKeys),
		platform.WithMigrations(migrations.FS, "."),
	)
	if err != nil {
		return nil, err
	}

	repo := postgres.NewAssetRepository(app.DB)
	uc := usecase.NewAssetUsecase(repo)
	httptransport.RegisterRoutes(app.Mux, httptransport.Deps{
		Auth:   app.Auth,
		Assets: httptransport.NewAssetHandler(uc),
	})

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
