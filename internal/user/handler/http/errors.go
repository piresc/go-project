package httptransport

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/user/usecase"
)

// User-service error codes. Shared codes come from gokit's response.Code*.
const (
	codeInvalidToken    = "ERR_INVALID_TOKEN"
	codeInvalidAudience = "ERR_INVALID_AUDIENCE"
	codeRefreshExpired  = "ERR_REFRESH_EXPIRED"
	codeInvalidRefresh  = "ERR_INVALID_REFRESH"
)

const (
	msgInvalidBody   = "Invalid request body"
	msgAuthRequired  = "Authentication required"
	msgInternalError = "Internal server error"
)

// writeUseCaseError maps a use-case error to an HTTP response. Unknown errors
// are logged and reported as 500 without leaking the cause.
func writeUseCaseError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidToken):
		response.Error(w, http.StatusUnauthorized, codeInvalidToken, "Invalid token")
	case errors.Is(err, usecase.ErrTokenExpired):
		response.Error(w, http.StatusUnauthorized, response.CodeTokenExpired, "Token expired")
	case errors.Is(err, usecase.ErrInvalidAudience):
		response.Error(w, http.StatusUnauthorized, codeInvalidAudience, "Invalid token audience")
	case errors.Is(err, usecase.ErrRefreshExpired):
		response.Error(w, http.StatusUnauthorized, codeRefreshExpired, "Refresh token expired")
	case errors.Is(err, usecase.ErrInvalidRefresh):
		response.Error(w, http.StatusUnauthorized, codeInvalidRefresh, "Invalid refresh token")
	case errors.Is(err, usecase.ErrUserNotFound):
		response.Error(w, http.StatusNotFound, response.CodeNotFound, "User not found")
	case errors.Is(err, usecase.ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, response.CodeInvalidInput, "Invalid input")
	default:
		log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusInternalServerError, response.CodeInternal, msgInternalError)
	}
}
