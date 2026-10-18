package httptransport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/piresc/gokit/middleware"
	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/asset/domain"
	"github.com/piresc/go-project/internal/asset/usecase"
)

// PortfolioReader captures portfolio reads.
type PortfolioReader interface {
	Get(ctx context.Context, userID string) (*domain.Portfolio, error)
}

// PortfolioHandler serves portfolio endpoints.
type PortfolioHandler struct {
	reader PortfolioReader
	log    *slog.Logger
}

// NewPortfolioHandler builds a PortfolioHandler.
func NewPortfolioHandler(reader PortfolioReader, log *slog.Logger) *PortfolioHandler {
	return &PortfolioHandler{reader: reader, log: log}
}

type portfolioResponse struct {
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Get handles GET /portfolio.
func (h *PortfolioHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r)
	if !ok {
		response.Error(w, http.StatusUnauthorized, "authentication required")
		return
	}
	p, err := h.reader.Get(r.Context(), claims.Subject)
	switch {
	case errors.Is(err, usecase.ErrNotFound):
		response.Error(w, http.StatusNotFound, "portfolio not found")
	case errors.Is(err, usecase.ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid input")
	case err != nil:
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
	default:
		response.Success(w, http.StatusOK, portfolioResponse{UserID: p.UserID, CreatedAt: p.CreatedAt})
	}
}
