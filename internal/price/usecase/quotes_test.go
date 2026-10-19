package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/piresc/go-project/internal/price/domain"
)

func TestQuotes_OrderOfSymbols(t *testing.T) {
	cache := &fakeCache{quotes: sevenQuotes()}
	got, err := NewQuotesUsecase(cache).Get(context.Background(), []string{"C", "A", "F"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []string{"C", "A", "F"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i, sym := range want {
		if got[i].Symbol != sym {
			t.Fatalf("got[%d] = %s, want %s", i, got[i].Symbol, sym)
		}
	}
}

func TestQuotes_UnknownLeftOut(t *testing.T) {
	cache := &fakeCache{quotes: sevenQuotes()}
	got, err := NewQuotesUsecase(cache).Get(context.Background(), []string{"A", "ZZZ", "B"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "A" || got[1].Symbol != "B" {
		t.Fatalf("got %+v, want A,B", got)
	}
}

func TestQuotes_DuplicatesCollapsed(t *testing.T) {
	cache := &fakeCache{quotes: sevenQuotes()}
	got, err := NewQuotesUsecase(cache).Get(context.Background(), []string{"B", "A", "B", "A"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "B" || got[1].Symbol != "A" {
		t.Fatalf("got %+v, want B,A", got)
	}
}

func TestQuotes_EmptyCache(t *testing.T) {
	got, err := NewQuotesUsecase(&fakeCache{}).Get(context.Background(), []string{"A"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", got)
	}
}

func TestQuotes_NoSymbolsNonNil(t *testing.T) {
	got, err := NewQuotesUsecase(&fakeCache{quotes: []domain.Quote{{Symbol: "A"}}}).Get(context.Background(), nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", got)
	}
}

func TestQuotes_CacheErrorPropagates(t *testing.T) {
	boom := errors.New("redis down")
	uc := NewQuotesUsecase(&fakeCache{getErr: boom})
	if _, err := uc.Get(context.Background(), []string{"A"}); !errors.Is(err, boom) {
		t.Fatalf("want cache error, got %v", err)
	}
}
