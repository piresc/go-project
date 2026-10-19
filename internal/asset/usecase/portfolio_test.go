package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/piresc/go-project/internal/asset/domain"
)

const testUserID = "11111111-1111-1111-1111-111111111111"

type fakePortfolioRepo struct {
	rows     map[string]*domain.Portfolio
	holdings map[string]float64
	creates  int
	upserts  int
	getErr   error
}

func newFakePortfolioRepo() *fakePortfolioRepo {
	return &fakePortfolioRepo{rows: map[string]*domain.Portfolio{}, holdings: map[string]float64{}}
}

func (f *fakePortfolioRepo) Get(_ context.Context, userID string) (*domain.Portfolio, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	p, ok := f.rows[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (f *fakePortfolioRepo) Create(_ context.Context, userID string) error {
	f.creates++
	f.rows[userID] = &domain.Portfolio{UserID: userID}
	return nil
}

func (f *fakePortfolioRepo) UpsertHoldingCapped(_ context.Context, _, symbol string, qty float64, limit int) error {
	f.upserts++
	if _, held := f.holdings[symbol]; !held && len(f.holdings) >= limit {
		return ErrTooManyHoldings
	}
	f.holdings[symbol] = qty
	return nil
}

func (f *fakePortfolioRepo) DeleteHolding(_ context.Context, _, symbol string) error {
	if _, ok := f.holdings[symbol]; !ok {
		return ErrNotFound
	}
	delete(f.holdings, symbol)
	return nil
}

func (f *fakePortfolioRepo) ListHoldings(context.Context, string) ([]domain.Holding, error) {
	var out []domain.Holding
	for s, q := range f.holdings {
		out = append(out, domain.Holding{Symbol: s, Quantity: q})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, nil
}

type fakePrices struct {
	quotes  []domain.Quote
	err     error
	calls   int
	symbols []string
}

func (f *fakePrices) Quotes(_ context.Context, symbols []string) ([]domain.Quote, error) {
	f.calls++
	f.symbols = symbols
	return f.quotes, f.err
}

func TestEnsurePortfolio_CreatesOnce(t *testing.T) {
	repo := newFakePortfolioRepo()
	uc := NewPortfolioUsecase(repo, nil)
	if err := uc.EnsurePortfolio(context.Background(), testUserID); err != nil {
		t.Fatalf("EnsurePortfolio: %v", err)
	}
	if repo.creates != 1 {
		t.Fatalf("creates = %d, want 1", repo.creates)
	}
}

func TestEnsurePortfolio_Redelivered(t *testing.T) {
	repo := newFakePortfolioRepo()
	uc := NewPortfolioUsecase(repo, nil)
	for range 2 {
		if err := uc.EnsurePortfolio(context.Background(), testUserID); err != nil {
			t.Fatalf("EnsurePortfolio: %v", err)
		}
	}
	if repo.creates != 1 {
		t.Fatalf("creates = %d, want 1", repo.creates)
	}
}

func TestEnsurePortfolio_InvalidID(t *testing.T) {
	repo := newFakePortfolioRepo()
	uc := NewPortfolioUsecase(repo, nil)
	err := uc.EnsurePortfolio(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	if repo.creates != 0 {
		t.Fatalf("creates = %d, want 0", repo.creates)
	}
}

func TestEnsurePortfolio_LookupErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	repo := newFakePortfolioRepo()
	repo.getErr = boom
	uc := NewPortfolioUsecase(repo, nil)
	if err := uc.EnsurePortfolio(context.Background(), testUserID); !errors.Is(err, boom) {
		t.Fatalf("want db error, got %v", err)
	}
}

func TestSetHolding_CreatesPortfolioAndNormalizesSymbol(t *testing.T) {
	repo := newFakePortfolioRepo()
	uc := NewPortfolioUsecase(repo, nil)
	h, err := uc.SetHolding(context.Background(), testUserID, " aapl ", 2.5)
	if err != nil {
		t.Fatalf("SetHolding: %v", err)
	}
	if h.Symbol != "AAPL" || h.Quantity != 2.5 {
		t.Fatalf("holding = %+v", h)
	}
	if repo.creates != 1 || repo.holdings["AAPL"] != 2.5 {
		t.Fatalf("creates=%d holdings=%v", repo.creates, repo.holdings)
	}
}

func TestSetHolding_Validation(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		symbol string
		qty    float64
	}{
		{"bad user", "nope", "AAPL", 1},
		{"empty symbol", testUserID, "", 1},
		{"bad symbol chars", testUserID, "AA PL", 1},
		{"symbol too long", testUserID, "ABCDEFGHIJK", 1},
		{"zero", testUserID, "AAPL", 0},
		{"negative", testUserID, "AAPL", -1},
		{"NaN", testUserID, "AAPL", math.NaN()},
		{"+Inf", testUserID, "AAPL", math.Inf(1)},
		{"too large", testUserID, "AAPL", 1e12 + 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newFakePortfolioRepo()
			_, err := NewPortfolioUsecase(repo, nil).SetHolding(context.Background(), c.user, c.symbol, c.qty)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
			if repo.creates != 0 || repo.upserts != 0 {
				t.Fatalf("repo touched: creates=%d upserts=%d", repo.creates, repo.upserts)
			}
		})
	}
}

func TestSetHolding_AcceptsMaxQuantity(t *testing.T) {
	repo := newFakePortfolioRepo()
	if _, err := NewPortfolioUsecase(repo, nil).SetHolding(context.Background(), testUserID, "BRK.B", 1e12); err != nil {
		t.Fatalf("SetHolding: %v", err)
	}
}

func TestSetHolding_Limit(t *testing.T) {
	repo := newFakePortfolioRepo()
	for i := range MaxHoldings {
		repo.holdings[fmt.Sprintf("S%d", i)] = 1
	}
	uc := NewPortfolioUsecase(repo, nil)
	if _, err := uc.SetHolding(context.Background(), testUserID, "NEW", 1); !errors.Is(err, ErrTooManyHoldings) {
		t.Fatalf("new symbol at limit: want ErrTooManyHoldings, got %v", err)
	}
	if _, err := uc.SetHolding(context.Background(), testUserID, "s7", 9); err != nil {
		t.Fatalf("update at limit: %v", err)
	}
	if repo.holdings["S7"] != 9 || len(repo.holdings) != MaxHoldings {
		t.Fatalf("holdings = %v", repo.holdings)
	}
}

func TestRemoveHolding(t *testing.T) {
	repo := newFakePortfolioRepo()
	repo.holdings["AAPL"] = 1
	uc := NewPortfolioUsecase(repo, nil)
	if err := uc.RemoveHolding(context.Background(), testUserID, "aapl"); err != nil {
		t.Fatalf("RemoveHolding: %v", err)
	}
	if err := uc.RemoveHolding(context.Background(), testUserID, "AAPL"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: want ErrNotFound, got %v", err)
	}
	if err := uc.RemoveHolding(context.Background(), testUserID, "bad symbol"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad symbol: want ErrInvalidInput, got %v", err)
	}
	if err := uc.RemoveHolding(context.Background(), "nope", "AAPL"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad user: want ErrInvalidInput, got %v", err)
	}
}

func TestHoldings_NeverNil(t *testing.T) {
	got, err := NewPortfolioUsecase(newFakePortfolioRepo(), nil).Holdings(context.Background(), testUserID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want empty non-nil", got, err)
	}
}

func TestValue_Math(t *testing.T) {
	repo := newFakePortfolioRepo()
	repo.rows[testUserID] = &domain.Portfolio{UserID: testUserID}
	repo.holdings = map[string]float64{"AAPL": 2, "MSFT": 0.5, "ZZZZ": 3, "AAAA": 1}
	prices := &fakePrices{quotes: []domain.Quote{{Symbol: "AAPL", Price: 100}, {Symbol: "MSFT", Price: 400}}}

	v, err := NewPortfolioUsecase(repo, prices).Value(context.Background(), testUserID)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if prices.calls != 1 || !reflect.DeepEqual(prices.symbols, []string{"AAAA", "AAPL", "MSFT", "ZZZZ"}) {
		t.Fatalf("price calls = %d symbols = %v", prices.calls, prices.symbols)
	}
	want := []domain.HoldingValue{
		{Symbol: "AAPL", Quantity: 2, Price: 100, Value: 200},
		{Symbol: "MSFT", Quantity: 0.5, Price: 400, Value: 200},
	}
	if !reflect.DeepEqual(v.Holdings, want) || v.Total != 400 {
		t.Fatalf("valuation = %+v", v)
	}
	if !reflect.DeepEqual(v.Unpriced, []string{"AAAA", "ZZZZ"}) {
		t.Fatalf("unpriced = %v", v.Unpriced)
	}
}

func TestValue_NoHoldingsSkipsPrices(t *testing.T) {
	repo := newFakePortfolioRepo()
	repo.rows[testUserID] = &domain.Portfolio{UserID: testUserID}
	prices := &fakePrices{err: errors.New("must not be called")}
	v, err := NewPortfolioUsecase(repo, prices).Value(context.Background(), testUserID)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if prices.calls != 0 || v.Total != 0 || len(v.Holdings) != 0 || len(v.Unpriced) != 0 {
		t.Fatalf("calls=%d valuation=%+v", prices.calls, v)
	}
}

func TestValue_PortfolioMissing(t *testing.T) {
	prices := &fakePrices{}
	_, err := NewPortfolioUsecase(newFakePortfolioRepo(), prices).Value(context.Background(), testUserID)
	if !errors.Is(err, ErrNotFound) || prices.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, prices.calls)
	}
}

func TestValue_PriceErrorIsUnavailable(t *testing.T) {
	boom := errors.New("upstream down")
	repo := newFakePortfolioRepo()
	repo.rows[testUserID] = &domain.Portfolio{UserID: testUserID}
	repo.holdings["AAPL"] = 1
	_, err := NewPortfolioUsecase(repo, &fakePrices{err: boom}).Value(context.Background(), testUserID)
	if !errors.Is(err, ErrPricesUnavailable) || !errors.Is(err, boom) {
		t.Fatalf("want ErrPricesUnavailable wrapping cause, got %v", err)
	}
}
