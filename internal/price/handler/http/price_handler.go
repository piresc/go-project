package httptransport

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/price/domain"
)

const topPerformersCount = 5

// TopReader ranks the cached quotes.
type TopReader interface {
	Top(ctx context.Context, n int) ([]domain.Quote, error)
}

// PriceHandler serves price endpoints.
type PriceHandler struct {
	top TopReader
	log *slog.Logger
}

// NewPriceHandler builds a PriceHandler.
func NewPriceHandler(top TopReader, log *slog.Logger) *PriceHandler {
	return &PriceHandler{top: top, log: log}
}

type quoteResponse struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	ChangePct float64 `json:"change_pct"`
}

// TopPerformers handles GET /prices/top-performers.
func (h *PriceHandler) TopPerformers(w http.ResponseWriter, r *http.Request) {
	quotes, err := h.top.Top(r.Context(), topPerformersCount)
	if err != nil {
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	out := make([]quoteResponse, 0, len(quotes))
	for _, q := range quotes {
		out = append(out, quoteResponse{Symbol: q.Symbol, Price: q.Price, ChangePct: q.ChangePct})
	}
	response.Success(w, http.StatusOK, out)
}
