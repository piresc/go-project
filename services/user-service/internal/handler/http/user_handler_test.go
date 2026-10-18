package httptransport

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/piresc/go-project/services/user-service/internal/domain"
	"github.com/piresc/go-project/services/user-service/internal/usecase"
)

// memRepo is an in-memory usecase.UserRepository; only the methods used by
// UserUseCase are implemented, the embedded interface covers the rest.
type memRepo struct {
	usecase.UserRepository
	user        domain.User
	updateCalls int
}

func (r *memRepo) FindByID(context.Context, string) (*domain.User, error) {
	u := r.user
	return &u, nil
}

func (r *memRepo) UpdateDisplayName(_ context.Context, _, name string) error {
	r.updateCalls++
	r.user.DisplayName = name
	return nil
}

type nopCache struct{}

func (nopCache) GetUser(context.Context, string) (*domain.User, bool, error) { return nil, false, nil }
func (nopCache) SetUser(context.Context, *domain.User) error                 { return nil }
func (nopCache) DeleteUser(context.Context, string) error                    { return nil }

func newRealUserEnv(t *testing.T) (*routerEnv, *memRepo, string) {
	t.Helper()
	repo := &memRepo{user: domain.User{ID: "u1", Email: "e@x.com", DisplayName: "old", CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	uc := usecase.NewUserUseCase(repo, nopCache{}, discardLogger())
	e := newRouterEnvWith(t, uc, uc)
	tok, _, _ := e.token(t)
	return e, repo, tok
}

func TestUpdateMeBadRequests(t *testing.T) {
	cases := []struct{ name, body, msg string }{
		{"empty object", `{}`, "invalid input"},
		{"blank name", `{"display_name":"   "}`, "invalid input"},
		{"unknown field", `{"display_name":"x","extra":1}`, "invalid request body"},
		{"empty body", ``, "invalid request body"},
		{"not json", `nope`, "invalid request body"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, repo, tok := newRealUserEnv(t)
			rr := e.do(http.MethodPatch, "/users/me", c.body, tok)
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), c.msg) {
				t.Fatalf("got %d %s", rr.Code, rr.Body.String())
			}
			if repo.updateCalls != 0 {
				t.Fatalf("UpdateDisplayName called %d times", repo.updateCalls)
			}
		})
	}
}

func TestUpdateMeValid(t *testing.T) {
	e, repo, tok := newRealUserEnv(t)
	rr := e.do(http.MethodPatch, "/users/me", `{"display_name":"  Neo  "}`, tok)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"display_name":"Neo"`) {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
	if repo.updateCalls != 1 || repo.user.DisplayName != "Neo" {
		t.Fatalf("repo state: calls=%d name=%q", repo.updateCalls, repo.user.DisplayName)
	}
}

func TestGetMeReturnsProfile(t *testing.T) {
	e := newRouterEnv(t)
	tok, _, _ := e.token(t)
	rr := e.do(http.MethodGet, "/users/me", "", tok)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"member_since"`) {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}
