package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/piresc/gokit/middleware"
	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/asset/domain"
	"github.com/piresc/go-project/internal/asset/usecase"
)

// PortfolioService captures the portfolio operations the handler serves.
type PortfolioService interface {
	Get(ctx context.Context, userID string) (*domain.Portfolio, error)
	Holdings(ctx context.Context, userID string) ([]domain.Holding, error)
	SetHolding(ctx context.Context, userID, symbol string, qty float64) (*domain.Holding, error)
	RemoveHolding(ctx context.Context, userID, symbol string) error
	Value(ctx context.Context, userID string) (*domain.Valuation, error)
}

// PortfolioHandler serves portfolio endpoints.
type PortfolioHandler struct {
	svc PortfolioService
	log *slog.Logger
}

// NewPortfolioHandler builds a PortfolioHandler.
func NewPortfolioHandler(svc PortfolioService, log *slog.Logger) *PortfolioHandler {
	return &PortfolioHandler{svc: svc, log: log}
}

const maxBodyBytes = 1 << 10

type holdingResponse struct {
	Symbol   string  `json:"symbol"`
	Quantity float64 `json:"quantity"`
}

type portfolioResponse struct {
	UserID    string            `json:"user_id"`
	CreatedAt time.Time         `json:"created_at"`
	Holdings  []holdingResponse `json:"holdings"`
}

type holdingValueResponse struct {
	Symbol   string  `json:"symbol"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Value    float64 `json:"value"`
}

type valuationResponse struct {
	Holdings []holdingValueResponse `json:"holdings"`
	Total    float64                `json:"total"`
	Unpriced []string               `json:"unpriced"`
}

type setHoldingRequest struct {
	Quantity *float64 `json:"quantity"`
}

// Get handles GET /portfolio.
func (h *PortfolioHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Get(r.Context(), userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	holdings, err := h.svc.Holdings(r.Context(), userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]holdingResponse, 0, len(holdings))
	for _, hd := range holdings {
		out = append(out, holdingResponse{Symbol: hd.Symbol, Quantity: hd.Quantity})
	}
	response.Success(w, http.StatusOK, portfolioResponse{UserID: p.UserID, CreatedAt: p.CreatedAt, Holdings: out})
}

// SetHolding handles PUT /portfolio/holdings/{symbol}.
func (h *PortfolioHandler) SetHolding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	var req setHoldingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil || req.Quantity == nil {
		response.Error(w, http.StatusBadRequest, "invalid input")
		return
	}
	hd, err := h.svc.SetHolding(r.Context(), userID, r.PathValue("symbol"), *req.Quantity)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	response.Success(w, http.StatusOK, holdingResponse{Symbol: hd.Symbol, Quantity: hd.Quantity})
}

// RemoveHolding handles DELETE /portfolio/holdings/{symbol}.
func (h *PortfolioHandler) RemoveHolding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	err := h.svc.RemoveHolding(r.Context(), userID, r.PathValue("symbol"))
	if errors.Is(err, usecase.ErrNotFound) {
		response.Error(w, http.StatusNotFound, "holding not found")
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Value handles GET /portfolio/value.
func (h *PortfolioHandler) Value(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Value(r.Context(), userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := valuationResponse{
		Holdings: make([]holdingValueResponse, 0, len(v.Holdings)),
		Total:    v.Total,
		Unpriced: v.Unpriced,
	}
	if out.Unpriced == nil {
		out.Unpriced = []string{}
	}
	for _, hv := range v.Holdings {
		out.Holdings = append(out.Holdings, holdingValueResponse{Symbol: hv.Symbol, Quantity: hv.Quantity, Price: hv.Price, Value: hv.Value})
	}
	response.Success(w, http.StatusOK, out)
}

func (h *PortfolioHandler) userID(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims, ok := middleware.GetClaims(r)
	if !ok {
		response.Error(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return claims.Subject, true
}

func (h *PortfolioHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, usecase.ErrNotFound):
		response.Error(w, http.StatusNotFound, "portfolio not found")
	case errors.Is(err, usecase.ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, usecase.ErrTooManyHoldings):
		response.Error(w, http.StatusConflict, "too many holdings")
	case errors.Is(err, usecase.ErrPricesUnavailable):
		h.log.WarnContext(r.Context(), "prices unavailable", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusServiceUnavailable, "prices unavailable")
	default:
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
	}
}
