// Package app wires the asset-service on top of pkg/platform.
package app

import (
	eventhandler "github.com/piresc/go-project/internal/asset/handler/event"
	httptransport "github.com/piresc/go-project/internal/asset/handler/http"
	"github.com/piresc/go-project/internal/asset/migrations"
	"github.com/piresc/go-project/internal/asset/repository/postgres"
	"github.com/piresc/go-project/internal/asset/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

const (
	serviceName         = "asset-service"
	userCreatedConsumer = "asset-service-user-created"
)

// App is the assembled asset-service.
type App struct {
	app *platform.App[Config]
}

// New loads configuration, runs migrations and wires routes.
func New() (*App, error) {
	app, err := platform.New[Config](serviceName,
		platform.Require(platform.Postgres, platform.Redis, platform.NATS, platform.AuthKeys),
		platform.WithMigrations(migrations.FS, "."),
	)
	if err != nil {
		return nil, err
	}

	repo := postgres.NewAssetRepository(app.DB)
	uc := usecase.NewAssetUsecase(repo)
	portfolioUC := usecase.NewPortfolioUsecase(postgres.NewPortfolioRepository(app.DB))
	httptransport.RegisterRoutes(app.Mux, httptransport.Deps{
		Auth:      app.Auth,
		Assets:    httptransport.NewAssetHandler(uc),
		Portfolio: httptransport.NewPortfolioHandler(portfolioUC, app.Logger),
	})

	if err := app.Events.Subscribe(userCreatedConsumer, eventhandler.NewUserCreated(portfolioUC).Handle); err != nil {
		return nil, err
	}

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
