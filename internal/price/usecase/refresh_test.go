package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/piresc/go-project/internal/price/domain"
)

var errUnavailable = errors.New("market unavailable")

type fakeSource struct {
	quotes []domain.Quote
	err    error
	calls  int
}

func (f *fakeSource) Quotes(context.Context) ([]domain.Quote, error) {
	f.calls++
	return f.quotes, f.err
}

type fakeCache struct {
	quotes []domain.Quote
	sets   int
	getErr error
}

func (f *fakeCache) Set(_ context.Context, q []domain.Quote) error {
	f.sets++
	f.quotes = q
	return nil
}

func (f *fakeCache) Get(context.Context) ([]domain.Quote, error) {
	return f.quotes, f.getErr
}

type fakeLock struct{ released int }

func (l *fakeLock) Release(context.Context) error {
	l.released++
	return nil
}

type fakeLocker struct {
	err  error
	keys []string
	lock *fakeLock
}

func (f *fakeLocker) Acquire(_ context.Context, key string) (Lock, error) {
	f.keys = append(f.keys, key)
	if f.err != nil {
		return nil, f.err
	}
	f.lock = &fakeLock{}
	return f.lock, nil
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func sevenQuotes() []domain.Quote {
	return []domain.Quote{
		{Symbol: "A", ChangePct: 1.0},
		{Symbol: "B", ChangePct: -2.0},
		{Symbol: "C", ChangePct: 5.5},
		{Symbol: "D", ChangePct: 3.2},
		{Symbol: "E", ChangePct: 0.4},
		{Symbol: "F", ChangePct: 4.1},
		{Symbol: "G", ChangePct: 2.2},
	}
}

func TestRefresh_CachesAndReleases(t *testing.T) {
	src := &fakeSource{quotes: sevenQuotes()}
	cache := &fakeCache{}
	locker := &fakeLocker{}
	uc := NewRefreshUsecase(src, cache, locker, discardLog())

	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if cache.sets != 1 || len(cache.quotes) != 7 {
		t.Fatalf("cache sets=%d quotes=%d, want 1 and 7", cache.sets, len(cache.quotes))
	}
	if len(locker.keys) != 1 || locker.keys[0] != "refresh" {
		t.Fatalf("lock keys = %v, want [refresh]", locker.keys)
	}
	if locker.lock.released != 1 {
		t.Fatalf("released = %d, want 1", locker.lock.released)
	}
}

func TestRefresh_LockHeldSkips(t *testing.T) {
	src := &fakeSource{quotes: sevenQuotes()}
	cache := &fakeCache{}
	locker := &fakeLocker{err: ErrLockHeld}
	uc := NewRefreshUsecase(src, cache, locker, discardLog())

	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh with held lock = %v, want nil", err)
	}
	if src.calls != 0 || cache.sets != 0 {
		t.Fatalf("source calls=%d cache sets=%d, want 0 and 0", src.calls, cache.sets)
	}
}

func TestRefresh_LockErrorPropagates(t *testing.T) {
	boom := errors.New("redis down")
	src := &fakeSource{}
	uc := NewRefreshUsecase(src, &fakeCache{}, &fakeLocker{err: boom}, discardLog())

	if err := uc.Refresh(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("want lock error, got %v", err)
	}
	if src.calls != 0 {
		t.Fatalf("source calls = %d, want 0", src.calls)
	}
}

func TestRefresh_BreakerOpenFailsFast(t *testing.T) {
	src := &fakeSource{err: errUnavailable}
	cache := &fakeCache{}
	locker := &fakeLocker{}
	uc := NewRefreshUsecase(src, cache, locker, discardLog())

	err := uc.Refresh(context.Background())
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if cache.sets != 0 {
		t.Fatalf("cache sets = %d, want 0", cache.sets)
	}
	if locker.lock.released != 1 {
		t.Fatalf("lock released = %d, want 1 even on failure", locker.lock.released)
	}
}

func TestRefresh_EmptyQuotesKeepsCache(t *testing.T) {
	prev := sevenQuotes()
	src := &fakeSource{quotes: nil}
	cache := &fakeCache{quotes: prev}
	locker := &fakeLocker{}
	uc := NewRefreshUsecase(src, cache, locker, discardLog())

	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if cache.sets != 0 || len(cache.quotes) != len(prev) {
		t.Fatalf("cache sets=%d quotes=%d, want 0 and %d", cache.sets, len(cache.quotes), len(prev))
	}
	if locker.lock.released != 1 {
		t.Fatalf("lock released = %d, want 1", locker.lock.released)
	}
}
