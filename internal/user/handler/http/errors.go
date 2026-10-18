package httptransport

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/user/usecase"
)

const msgInvalidBody = "invalid request body"

// writeUseCaseError maps a use-case error to an HTTP response. Unknown errors
// are logged and reported as 500 without leaking the cause.
func writeUseCaseError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidToken):
		response.Error(w, http.StatusUnauthorized, "invalid token")
	case errors.Is(err, usecase.ErrTokenExpired):
		response.Error(w, http.StatusUnauthorized, "token expired")
	case errors.Is(err, usecase.ErrInvalidAudience):
		response.Error(w, http.StatusUnauthorized, "invalid token audience")
	case errors.Is(err, usecase.ErrRefreshExpired):
		response.Error(w, http.StatusUnauthorized, "refresh token expired")
	case errors.Is(err, usecase.ErrInvalidRefresh):
		response.Error(w, http.StatusUnauthorized, "invalid refresh token")
	case errors.Is(err, usecase.ErrUserNotFound):
		response.Error(w, http.StatusNotFound, "user not found")
	case errors.Is(err, usecase.ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid input")
	default:
		log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
	}
}
