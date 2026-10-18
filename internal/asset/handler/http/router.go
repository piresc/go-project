// Package httptransport is the HTTP transport layer: routes and handlers only.
package httptransport

import "net/http"

// Deps are the handlers and middleware the router needs.
type Deps struct {
	Auth      func(http.Handler) http.Handler
	Assets    *AssetHandler
	Portfolio *PortfolioHandler
}

// RegisterRoutes mounts the asset-service routes on mux.
func RegisterRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /assets", d.Auth(http.HandlerFunc(d.Assets.List)))
	mux.Handle("GET /portfolio", d.Auth(http.HandlerFunc(d.Portfolio.Get)))
}
