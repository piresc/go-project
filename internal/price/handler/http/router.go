// Package httptransport is the HTTP transport layer: routes and handlers only.
package httptransport

import "net/http"

// Deps are the handlers and middleware the router needs.
type Deps struct {
	OptionalAuth func(http.Handler) http.Handler
	Prices       *PriceHandler
}

// RegisterRoutes mounts the price-service routes on mux.
func RegisterRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /prices/top-performers", d.OptionalAuth(http.HandlerFunc(d.Prices.TopPerformers)))
	mux.Handle("GET /prices/quotes", d.OptionalAuth(http.HandlerFunc(d.Prices.Quotes)))
}
