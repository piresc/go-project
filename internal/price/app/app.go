// Package app wires the price-service on top of pkg/platform.
package app

import (
	"github.com/piresc/go-project/internal/price/gateway/market"
	httptransport "github.com/piresc/go-project/internal/price/handler/http"
	"github.com/piresc/go-project/internal/price/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

const serviceName = "price-service"

// App is the assembled price-service.
type App struct {
	app *platform.App[Config]
}

// New loads configuration and wires routes.
func New() (*App, error) {
	app, err := platform.New[Config](serviceName,
		platform.Require(platform.Redis, platform.AuthKeys),
	)
	if err != nil {
		return nil, err
	}

	uc := usecase.NewPriceUsecase(market.NewClient())
	httptransport.RegisterRoutes(app.Mux, httptransport.Deps{
		OptionalAuth: app.OptionalAuth,
		Prices:       httptransport.NewPriceHandler(uc),
	})

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
