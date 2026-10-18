package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func newMux(top TopReader) *http.ServeMux {
	mux := http.NewServeMux()
	passthrough := func(h http.Handler) http.Handler { return h }
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	RegisterRoutes(mux, Deps{OptionalAuth: passthrough, Prices: NewPriceHandler(top, log)})
	return mux
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
