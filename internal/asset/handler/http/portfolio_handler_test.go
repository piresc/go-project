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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/middleware"

	"github.com/piresc/go-project/internal/asset/domain"
	"github.com/piresc/go-project/internal/asset/usecase"
)

const testUser = "11111111-1111-1111-1111-111111111111"

type fakeService struct {
	portfolio *domain.Portfolio
	holdings  []domain.Holding
	valuation *domain.Valuation
	err       error

	gotSymbol string
	gotQty    float64
}

func (f *fakeService) Get(context.Context, string) (*domain.Portfolio, error) {
	return f.portfolio, f.err
}

func (f *fakeService) Holdings(context.Context, string) ([]domain.Holding, error) {
	return f.holdings, nil
}

func (f *fakeService) SetHolding(_ context.Context, _, symbol string, qty float64) (*domain.Holding, error) {
	f.gotSymbol, f.gotQty = symbol, qty
	if f.err != nil {
		return nil, f.err
	}
	return &domain.Holding{Symbol: strings.ToUpper(symbol), Quantity: qty}, nil
}

func (f *fakeService) RemoveHolding(context.Context, string, string) error { return f.err }

func (f *fakeService) Value(context.Context, string) (*domain.Valuation, error) {
	return f.valuation, f.err
}

// do serves the request through the real router with claims injected in
// place of the Auth middleware. A nil claims user means unauthenticated.
func do(t *testing.T, svc *fakeService, method, path, body string, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authed {
				claims := &auth.RSAClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: testUser}}
				r = r.WithContext(context.WithValue(r.Context(), middleware.KeyClaims, claims))
			}
			next.ServeHTTP(w, r)
		})
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	RegisterRoutes(mux, Deps{Auth: inject, Assets: NewAssetHandler(nil), Portfolio: NewPortfolioHandler(svc, log)})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func dataOf(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return env.Data
}

func TestGetPortfolio_IncludesHoldings(t *testing.T) {
	svc := &fakeService{
		portfolio: &domain.Portfolio{UserID: testUser, CreatedAt: time.Unix(0, 0)},
		holdings:  []domain.Holding{{Symbol: "AAPL", Quantity: 2}},
	}
	rr := do(t, svc, http.MethodGet, "/portfolio", "", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	d := dataOf(t, rr)
	hs, _ := d["holdings"].([]any)
	if d["user_id"] != testUser || d["created_at"] == nil || len(hs) != 1 {
		t.Fatalf("data = %v", d)
	}
}

func TestGetPortfolio_EmptyHoldingsIsArray(t *testing.T) {
	svc := &fakeService{portfolio: &domain.Portfolio{UserID: testUser}}
	rr := do(t, svc, http.MethodGet, "/portfolio", "", true)
	if !strings.Contains(rr.Body.String(), `"holdings":[]`) {
		t.Fatalf("body = %s", rr.Body)
	}
}

func TestPortfolio_Statuses(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		authed bool
		err    error
		want   int
		msg    string
	}{
		{"get unauthenticated", http.MethodGet, "/portfolio", "", false, nil, 401, "authentication required"},
		{"get not found", http.MethodGet, "/portfolio", "", true, usecase.ErrNotFound, 404, "portfolio not found"},
		{"get internal", http.MethodGet, "/portfolio", "", true, errors.New("boom"), 500, "internal server error"},
		{"put ok", http.MethodPut, "/portfolio/holdings/aapl", `{"quantity":2}`, true, nil, 200, `"quantity":2`},
		{"put unauthenticated", http.MethodPut, "/portfolio/holdings/aapl", `{"quantity":2}`, false, nil, 401, ""},
		{"put bad json", http.MethodPut, "/portfolio/holdings/aapl", `{`, true, nil, 400, "invalid input"},
		{"put missing quantity", http.MethodPut, "/portfolio/holdings/aapl", `{}`, true, nil, 400, "invalid input"},
		{"put empty body", http.MethodPut, "/portfolio/holdings/aapl", ``, true, nil, 400, "invalid input"},
		{"put invalid", http.MethodPut, "/portfolio/holdings/aapl", `{"quantity":-1}`, true, usecase.ErrInvalidInput, 400, "invalid input"},
		{"put too many", http.MethodPut, "/portfolio/holdings/aapl", `{"quantity":1}`, true, usecase.ErrTooManyHoldings, 409, "too many holdings"},
		{"put internal", http.MethodPut, "/portfolio/holdings/aapl", `{"quantity":1}`, true, errors.New("boom"), 500, "internal server error"},
		{"delete ok", http.MethodDelete, "/portfolio/holdings/AAPL", "", true, nil, 204, ""},
		{"delete missing", http.MethodDelete, "/portfolio/holdings/AAPL", "", true, usecase.ErrNotFound, 404, "holding not found"},
		{"delete invalid", http.MethodDelete, "/portfolio/holdings/bad!", "", true, usecase.ErrInvalidInput, 400, "invalid input"},
		{"delete unauthenticated", http.MethodDelete, "/portfolio/holdings/AAPL", "", false, nil, 401, ""},
		{"value not found", http.MethodGet, "/portfolio/value", "", true, usecase.ErrNotFound, 404, "portfolio not found"},
		{"value unavailable", http.MethodGet, "/portfolio/value", "", true, usecase.ErrPricesUnavailable, 503, "prices unavailable"},
		{"value internal", http.MethodGet, "/portfolio/value", "", true, errors.New("boom"), 500, "internal server error"},
		{"value unauthenticated", http.MethodGet, "/portfolio/value", "", false, nil, 401, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeService{err: c.err, portfolio: &domain.Portfolio{UserID: testUser}, valuation: &domain.Valuation{}}
			rr := do(t, svc, c.method, c.path, c.body, c.authed)
			if rr.Code != c.want || !strings.Contains(rr.Body.String(), c.msg) {
				t.Fatalf("got %d %s; want %d containing %q", rr.Code, rr.Body, c.want, c.msg)
			}
		})
	}
}

func TestPutHolding_PassesSymbolAndQuantity(t *testing.T) {
	svc := &fakeService{}
	rr := do(t, svc, http.MethodPut, "/portfolio/holdings/brk.b", `{"quantity":1.5}`, true)
	if rr.Code != http.StatusOK || svc.gotSymbol != "brk.b" || svc.gotQty != 1.5 {
		t.Fatalf("got %d %s symbol=%q qty=%v", rr.Code, rr.Body, svc.gotSymbol, svc.gotQty)
	}
	d := dataOf(t, rr)
	if d["symbol"] != "BRK.B" || d["quantity"] != 1.5 {
		t.Fatalf("data = %v", d)
	}
}

func TestValue_Response(t *testing.T) {
	svc := &fakeService{valuation: &domain.Valuation{
		Holdings: []domain.HoldingValue{{Symbol: "AAPL", Quantity: 2, Price: 100, Value: 200}},
		Total:    200,
		Unpriced: []string{"ZZZZ"},
	}}
	rr := do(t, svc, http.MethodGet, "/portfolio/value", "", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	d := dataOf(t, rr)
	hs, _ := d["holdings"].([]any)
	first, _ := hs[0].(map[string]any)
	un, _ := d["unpriced"].([]any)
	if d["total"] != 200.0 || first["symbol"] != "AAPL" || first["quantity"] != 2.0 || first["price"] != 100.0 || first["value"] != 200.0 || len(un) != 1 {
		t.Fatalf("data = %v", d)
	}
}

func TestValue_EmptyIsArrays(t *testing.T) {
	rr := do(t, &fakeService{valuation: &domain.Valuation{}}, http.MethodGet, "/portfolio/value", "", true)
	body := rr.Body.String()
	if !strings.Contains(body, `"holdings":[]`) || !strings.Contains(body, `"unpriced":[]`) || !strings.Contains(body, `"total":0`) {
		t.Fatalf("body = %s", body)
	}
}
