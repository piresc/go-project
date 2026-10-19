package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/piresc/go-project/internal/price/domain"
)

type fakeTop struct {
	quotes []domain.Quote
	err    error
	n      int
}

func (f *fakeTop) Top(_ context.Context, n int) ([]domain.Quote, error) {
	f.n = n
	return f.quotes, f.err
}

type fakeQuotes struct {
	quotes  []domain.Quote
	err     error
	symbols []string
	calls   int
}

func (f *fakeQuotes) Get(_ context.Context, symbols []string) ([]domain.Quote, error) {
	f.calls++
	f.symbols = symbols
	return f.quotes, f.err
}

func newMux(top TopReader) *http.ServeMux {
	return newMuxQuotes(top, &fakeQuotes{})
}

func newMuxQuotes(top TopReader, quotes QuotesReader) *http.ServeMux {
	mux := http.NewServeMux()
	passthrough := func(h http.Handler) http.Handler { return h }
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	RegisterRoutes(mux, Deps{OptionalAuth: passthrough, Prices: NewPriceHandler(top, quotes, log)})
	return mux
}

func TestQuotes_OK(t *testing.T) {
	q := &fakeQuotes{quotes: []domain.Quote{{Symbol: "AAPL", Price: 10, ChangePct: 1.5}}}
	rec := httptest.NewRecorder()
	newMuxQuotes(&fakeTop{}, q).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/prices/quotes?symbols=aapl,%20nvda,,", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(q.symbols) != 2 || q.symbols[0] != "AAPL" || q.symbols[1] != "NVDA" {
		t.Fatalf("symbols = %v, want [AAPL NVDA]", q.symbols)
	}
	var body struct {
		Data []struct {
			Symbol    string  `json:"symbol"`
			Price     float64 `json:"price"`
			ChangePct float64 `json:"change_pct"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].Symbol != "AAPL" || body.Data[0].ChangePct != 1.5 {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestQuotes_BadRequests(t *testing.T) {
	fifty := make([]string, 0, 51)
	for i := 0; i < 51; i++ {
		fifty = append(fifty, "A")
	}
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"missing", "", "Symbols is required"},
		{"only empties", "?symbols=,%20,", "Symbols is required"},
		{"too many", "?symbols=" + strings.Join(fifty, ","), "Too many symbols"},
		{"invalid chars", "?symbols=AAPL,BAD$", "Invalid symbol"},
		{"too long", "?symbols=ABCDEFGHIJK", "Invalid symbol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQuotes{}
			rec := httptest.NewRecorder()
			newMuxQuotes(&fakeTop{}, q).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/prices/quotes"+tt.query, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Fatalf("body = %s, want %q", rec.Body.String(), tt.want)
			}
			if q.calls != 0 {
				t.Fatalf("usecase called %d times, want 0", q.calls)
			}
		})
	}
}

func TestQuotes_FiftyAllowed(t *testing.T) {
	syms := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		syms = append(syms, "A")
	}
	rec := httptest.NewRecorder()
	newMuxQuotes(&fakeTop{}, &fakeQuotes{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/prices/quotes?symbols="+strings.Join(syms, ","), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestQuotes_Error(t *testing.T) {
	rec := httptest.NewRecorder()
	newMuxQuotes(&fakeTop{}, &fakeQuotes{err: errors.New("redis down")}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/prices/quotes?symbols=AAPL", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestTopPerformers_OK(t *testing.T) {
	top := &fakeTop{quotes: []domain.Quote{{Symbol: "C", Price: 10, ChangePct: 5.5}}}
	rec := httptest.NewRecorder()
	newMux(top).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/prices/top-performers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if top.n != 5 {
		t.Fatalf("n = %d, want 5", top.n)
	}
	var body struct {
		Data []struct {
			Symbol    string  `json:"symbol"`
			Price     float64 `json:"price"`
			ChangePct float64 `json:"change_pct"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].Symbol != "C" || body.Data[0].ChangePct != 5.5 {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestTopPerformers_Error(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(&fakeTop{err: errors.New("redis down")}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/prices/top-performers", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
