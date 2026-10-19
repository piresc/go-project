package usecase

import (
	"context"
	"errors"
	"testing"
)

func TestTopPerformers_ServesStaleCache(t *testing.T) {
	cache := &fakeCache{quotes: sevenQuotes()}
	src := &fakeSource{err: errUnavailable}
	uc := NewTopUsecase(cache)

	got, err := uc.Top(context.Background(), 5)
	if err != nil {
		t.Fatalf("Top: %v", err)
	}
	want := []string{"C", "F", "D", "G", "A"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, sym := range want {
		if got[i].Symbol != sym {
			t.Fatalf("got[%d] = %s, want %s (all: %+v)", i, got[i].Symbol, sym, got)
		}
	}
	if src.calls != 0 {
		t.Fatalf("source must not be consulted, calls = %d", src.calls)
	}
}

func TestTop_DoesNotMutateCache(t *testing.T) {
	cache := &fakeCache{quotes: sevenQuotes()}
	uc := NewTopUsecase(cache)
	if _, err := uc.Top(context.Background(), 3); err != nil {
		t.Fatalf("Top: %v", err)
	}
	if cache.quotes[0].Symbol != "A" {
		t.Fatalf("cached slice was reordered: %+v", cache.quotes)
	}
}

func TestTop_FewerThanN(t *testing.T) {
	cache := &fakeCache{quotes: sevenQuotes()[:2]}
	got, err := NewTopUsecase(cache).Top(context.Background(), 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v; want 2 quotes", got, err)
	}
}

func TestTop_EmptyCache(t *testing.T) {
	uc := NewTopUsecase(&fakeCache{})
	got, err := uc.Top(context.Background(), 5)
	if err != nil {
		t.Fatalf("Top: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", got)
	}
}

func TestTop_CacheErrorPropagates(t *testing.T) {
	boom := errors.New("redis down")
	uc := NewTopUsecase(&fakeCache{getErr: boom})
	if _, err := uc.Top(context.Background(), 5); !errors.Is(err, boom) {
		t.Fatalf("want cache error, got %v", err)
	}
}
