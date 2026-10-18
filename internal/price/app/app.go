// Package app wires the price-service on top of gokit bootstrap.
package app

import (
	"errors"
	"fmt"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/bootstrap"
	"github.com/piresc/gokit/middleware"

	"github.com/piresc/go-project/internal/price/gateway/market"
	httptransport "github.com/piresc/go-project/internal/price/handler/http"
	"github.com/piresc/go-project/internal/price/usecase"
)

const serviceName = "price-service"

// App is the assembled price-service.
type App struct {
	app *bootstrap.HTTPApp[Config]
}

// New loads configuration and wires routes.
func New() (*App, error) {
	app, err := bootstrap.NewHTTPService[Config](serviceName)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if app.Redis == nil {
		return nil, errors.New("redis is required for token revocation checks")
	}

	pub, err := auth.LoadRSAPublicKey(app.Config.Auth.PublicKeyPEM, app.Config.Auth.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load public key: %w", err)
	}

	mw := middleware.NewMiddleware(app.Logger, app.Tracer)
	authMW := mw.OptionalRS256Auth(auth.NewRSAVerifier(pub), middleware.RS256Options{
		Revocation: auth.NewRedisRevocationStore(app.Redis),
	})

	uc := usecase.NewPriceUsecase(market.NewClient())
	httptransport.RegisterRoutes(app.Mux, httptransport.Deps{
		OptionalAuth: authMW,
		Prices:       httptransport.NewPriceHandler(uc),
	})

	return &App{app: app}, nil
}

// Run serves HTTP until a termination signal is received.
func (a *App) Run() error {
	return a.app.Run()
}
