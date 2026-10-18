package eventhandler

import (
	"context"
	"errors"
	"testing"

	"github.com/piresc/go-project/internal/asset/domain"
	"github.com/piresc/go-project/internal/asset/usecase"
	"github.com/piresc/go-project/pkg/platform"
)

type stubRepo struct {
	createErr error
}

func (s stubRepo) Get(context.Context, string) (*domain.Portfolio, error) {
	return nil, usecase.ErrNotFound
}

func (s stubRepo) Create(context.Context, string) error { return s.createErr }

func TestUserCreated_MalformedIsPermanent(t *testing.T) {
	h := NewUserCreated(usecase.NewPortfolioUsecase(stubRepo{}))
	cases := map[string][]byte{
		"bad json":    []byte(`{`),
		"bad user id": []byte(`{"user_id":"nope"}`),
		"empty":       []byte(`{}`),
	}
	for name, data := range cases {
		err := h.Handle(context.Background(), platform.Event{Data: data})
		if !errors.Is(err, platform.ErrPermanent) {
			t.Fatalf("%s: want ErrPermanent, got %v", name, err)
		}
	}
}

func TestUserCreated_RepoErrorRetries(t *testing.T) {
	boom := errors.New("db down")
	h := NewUserCreated(usecase.NewPortfolioUsecase(stubRepo{createErr: boom}))
	err := h.Handle(context.Background(), platform.Event{
		Data: []byte(`{"user_id":"11111111-1111-1111-1111-111111111111"}`),
	})
	if err == nil || errors.Is(err, platform.ErrPermanent) {
		t.Fatalf("want retryable error, got %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped repo error, got %v", err)
	}
}

func TestUserCreated_Success(t *testing.T) {
	h := NewUserCreated(usecase.NewPortfolioUsecase(stubRepo{}))
	err := h.Handle(context.Background(), platform.Event{
		Data: []byte(`{"user_id":"11111111-1111-1111-1111-111111111111","email":"a@b.c","role":"investor"}`),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
}
