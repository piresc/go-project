// Package httptransport is the HTTP transport layer: routes and handlers only.
package httptransport

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/piresc/gokit/middleware"
	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/user/domain"
)

// Authenticator captures the auth use-case behavior used by HTTP handlers.
type Authenticator interface {
	AuthenticateGoogle(ctx context.Context, idToken string) (*domain.TokenPair, error)
	AuthenticateApple(ctx context.Context, idToken string) (*domain.TokenPair, error)
	RefreshTokens(ctx context.Context, refreshToken string) (*domain.TokenPair, error)
	Logout(ctx context.Context, userID, accessTokenJTI string, tokenExp time.Time) error
}

// AuthHandler serves authentication endpoints.
type AuthHandler struct {
	authenticator Authenticator
	log           *slog.Logger
}

// NewAuthHandler creates an auth handler.
func NewAuthHandler(authenticator Authenticator, log *slog.Logger) *AuthHandler {
	return &AuthHandler{authenticator: authenticator, log: log}
}

// Google handles POST /auth/google.
func (h *AuthHandler) Google(w http.ResponseWriter, r *http.Request) {
	h.idTokenAuth(w, r, h.authenticator.AuthenticateGoogle)
}

// Apple handles POST /auth/apple.
func (h *AuthHandler) Apple(w http.ResponseWriter, r *http.Request) {
	h.idTokenAuth(w, r, h.authenticator.AuthenticateApple)
}

func (h *AuthHandler) idTokenAuth(w http.ResponseWriter, r *http.Request, fn func(context.Context, string) (*domain.TokenPair, error)) {
	var req authRequest
	if err := decodeJSON(r, &req); err != nil || req.IDToken == "" {
		response.Error(w, http.StatusBadRequest, msgInvalidBody)
		return
	}
	pair, err := fn(r.Context(), req.IDToken)
	if err != nil {
		writeUseCaseError(w, r, h.log, err)
		return
	}
	response.Success(w, http.StatusOK, pair)
}

// Refresh handles POST /auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil || req.RefreshToken == "" {
		response.Error(w, http.StatusBadRequest, msgInvalidBody)
		return
	}
	pair, err := h.authenticator.RefreshTokens(r.Context(), req.RefreshToken)
	if err != nil {
		writeUseCaseError(w, r, h.log, err)
		return
	}
	response.Success(w, http.StatusOK, pair)
}

// Logout handles POST /auth/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r)
	if !ok {
		response.Error(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var exp time.Time
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	}
	if err := h.authenticator.Logout(r.Context(), claims.Subject, claims.ID, exp); err != nil {
		writeUseCaseError(w, r, h.log, err)
		return
	}
	response.Success(w, http.StatusOK, map[string]string{"message": "logged out successfully"})
}
