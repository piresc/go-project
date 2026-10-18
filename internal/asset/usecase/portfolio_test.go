package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/piresc/go-project/internal/asset/domain"
)

const testUserID = "11111111-1111-1111-1111-111111111111"

type fakePortfolioRepo struct {
	rows    map[string]*domain.Portfolio
	creates int
	getErr  error
}

func newFakePortfolioRepo() *fakePortfolioRepo {
	return &fakePortfolioRepo{rows: map[string]*domain.Portfolio{}}
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

func TestEnsurePortfolio_CreatesOnce(t *testing.T) {
	repo := newFakePortfolioRepo()
	uc := NewPortfolioUsecase(repo)
	if err := uc.EnsurePortfolio(context.Background(), testUserID); err != nil {
		t.Fatalf("EnsurePortfolio: %v", err)
	}
	if repo.creates != 1 {
		t.Fatalf("creates = %d, want 1", repo.creates)
	}
}

func TestEnsurePortfolio_Redelivered(t *testing.T) {
	repo := newFakePortfolioRepo()
	uc := NewPortfolioUsecase(repo)
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
	uc := NewPortfolioUsecase(repo)
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
	uc := NewPortfolioUsecase(repo)
	if err := uc.EnsurePortfolio(context.Background(), testUserID); !errors.Is(err, boom) {
		t.Fatalf("want db error, got %v", err)
	}
}
