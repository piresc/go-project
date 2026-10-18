package httptransport

import "net/http"

// RegisterRoutes mounts the user-service routes on mux. Auth routes
// /auth/google, /auth/apple and /auth/refresh are public; the rest are
// wrapped in authMW.
func RegisterRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler, auth *AuthHandler, user *UserHandler) {
	mux.HandleFunc("POST /auth/google", auth.Google)
	mux.HandleFunc("POST /auth/apple", auth.Apple)
	mux.HandleFunc("POST /auth/refresh", auth.Refresh)

	mux.Handle("POST /auth/logout", authMW(http.HandlerFunc(auth.Logout)))
	mux.Handle("GET /users/me", authMW(http.HandlerFunc(user.GetMe)))
	mux.Handle("PATCH /users/me", authMW(http.HandlerFunc(user.UpdateMe)))
}
