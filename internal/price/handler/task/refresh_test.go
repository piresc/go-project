package taskhandler

import (
	"context"
	"errors"
	"testing"
)

type fakeRefresher struct {
	err   error
	calls int
}

func (f *fakeRefresher) Refresh(context.Context) error {
	f.calls++
	return f.err
}

func TestRefresh_CallsUsecase(t *testing.T) {
	uc := &fakeRefresher{}
	if err := Refresh(uc)(context.Background(), nil); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if uc.calls != 1 {
		t.Fatalf("calls = %d, want 1", uc.calls)
	}
}

func TestRefresh_PropagatesError(t *testing.T) {
	boom := errors.New("boom")
	if err := Refresh(&fakeRefresher{err: boom})(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}
