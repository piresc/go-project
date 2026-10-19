package httptransport

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/price/domain"
)

const (
	topPerformersCount = 5
	maxQuoteSymbols    = 50
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9.\-]{1,10}$`)

// TopReader ranks the cached quotes.
type TopReader interface {
	Top(ctx context.Context, n int) ([]domain.Quote, error)
}

// QuotesReader looks up cached quotes by symbol.
type QuotesReader interface {
	Get(ctx context.Context, symbols []string) ([]domain.Quote, error)
}

// PriceHandler serves price endpoints.
type PriceHandler struct {
	top    TopReader
	quotes QuotesReader
	log    *slog.Logger
}

// NewPriceHandler builds a PriceHandler.
func NewPriceHandler(top TopReader, quotes QuotesReader, log *slog.Logger) *PriceHandler {
	return &PriceHandler{top: top, quotes: quotes, log: log}
}

type quoteResponse struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	ChangePct float64 `json:"change_pct"`
}

// Quotes handles GET /prices/quotes?symbols=AAPL,NVDA.
func (h *PriceHandler) Quotes(w http.ResponseWriter, r *http.Request) {
	var symbols []string
	for _, s := range strings.Split(r.URL.Query().Get("symbols"), ",") {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			symbols = append(symbols, s)
		}
	}
	if len(symbols) == 0 {
		response.Error(w, http.StatusBadRequest, "symbols is required")
		return
	}
	if len(symbols) > maxQuoteSymbols {
		response.Error(w, http.StatusBadRequest, "too many symbols")
		return
	}
	for _, s := range symbols {
		if !symbolPattern.MatchString(s) {
			response.Error(w, http.StatusBadRequest, "invalid symbol")
			return
		}
	}
	quotes, err := h.quotes.Get(r.Context(), symbols)
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
