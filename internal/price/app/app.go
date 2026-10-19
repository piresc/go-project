// Package app wires the price-service on top of pkg/platform.
package app

import (
	"github.com/piresc/go-project/internal/price/gateway/cache"
	"github.com/piresc/go-project/internal/price/gateway/lock"
	"github.com/piresc/go-project/internal/price/gateway/market"
	httptransport "github.com/piresc/go-project/internal/price/handler/http"
	taskhandler "github.com/piresc/go-project/internal/price/handler/task"
	"github.com/piresc/go-project/internal/price/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

const (
	serviceName      = "price-service"
	refreshTaskType  = "price:refresh"
	marketClientName = "market"
)

// App is the assembled price-service.
type App struct {
	app *platform.App[Config]
}

// New loads configuration, wires the refresh task and routes.
func New() (*App, error) {
	app, err := platform.New[Config](serviceName,
		platform.Require(platform.Redis, platform.Asynq, platform.AuthKeys),
	)
	if err != nil {
		return nil, err
	}

	marketClient, err := app.HTTPClient(marketClientName)
	if err != nil {
		return nil, err
	}

	quotes := cache.NewRedis(app.Redis)
	refreshUC := usecase.NewRefreshUsecase(market.New(marketClient), quotes, lock.New(app.Locker), app.Logger)
	topUC := usecase.NewTopUsecase(quotes)
	quotesUC := usecase.NewQuotesUsecase(quotes)

	app.Tasks.Handle(refreshTaskType, taskhandler.Refresh(refreshUC))
	httptransport.RegisterRoutes(app.Mux, httptransport.Deps{
		OptionalAuth: app.OptionalAuth,
		Prices:       httptransport.NewPriceHandler(topUC, quotesUC, app.Logger),
	})

	return &App{app: app}, nil
}

// Run serves HTTP and runs the refresh schedule until a termination signal is
// received.
func (a *App) Run() error {
	return a.app.Run()
}
