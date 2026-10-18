package httptransport

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/piresc/go-project/services/user-service/internal/usecase"
)

func TestGoogleAuthInvalidToken(t *testing.T) {
	e := newRouterEnv(t)
	e.auth.pair, e.auth.err = nil, usecase.ErrInvalidToken
	rr := e.do(http.MethodPost, "/auth/google", `{"id_token":"invalid-token"}`, "")
	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), "invalid token") {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAuthBadBodies(t *testing.T) {
	e := newRouterEnv(t)
	for _, body := range []string{``, `{}`, `{"id_token":"x","extra":1}`, `nope`} {
		rr := e.do(http.MethodPost, "/auth/google", body, "")
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "invalid request body") {
			t.Errorf("body %q: got %d %s", body, rr.Code, rr.Body.String())
		}
	}
}

func TestRefreshHappyPath(t *testing.T) {
	e := newRouterEnv(t)
	rr := e.do(http.MethodPost, "/auth/refresh", `{"refresh_token":"r1"}`, "")
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `"access_token":"a"`) || !strings.Contains(body, `"refresh_token":"r"`) {
		t.Fatalf("got %d %s", rr.Code, body)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
		msg  string
	}{
		{usecase.ErrTokenExpired, 401, "token expired"},
		{usecase.ErrInvalidAudience, 401, "invalid token audience"},
		{usecase.ErrRefreshExpired, 401, "refresh token expired"},
		{usecase.ErrInvalidRefresh, 401, "invalid refresh token"},
		{usecase.ErrUserNotFound, 404, "user not found"},
		{usecase.ErrInvalidInput, 400, "invalid input"},
		{errors.New("db password leaked"), 500, "internal server error"},
	}
	e := newRouterEnv(t)
	for _, c := range cases {
		e.auth.err = c.err
		rr := e.do(http.MethodPost, "/auth/refresh", `{"refresh_token":"r1"}`, "")
		if rr.Code != c.code || !strings.Contains(rr.Body.String(), c.msg) || strings.Contains(rr.Body.String(), "leaked") {
			t.Errorf("%v: got %d %s", c.err, rr.Code, rr.Body.String())
		}
	}
}

func TestLogoutPassesClaims(t *testing.T) {
	e := newRouterEnv(t)
	tok, jti, exp := e.token(t)
	rr := e.do(http.MethodPost, "/auth/logout", "", tok)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
	if !e.auth.logoutCall || e.auth.logoutUser != "u1" || e.auth.logoutJTI != jti {
		t.Fatalf("logout args: %+v", e.auth)
	}
	if d := e.auth.logoutExp.Sub(exp); d > 1e9 || d < -1e9 {
		t.Fatalf("exp mismatch: %v vs %v", e.auth.logoutExp, exp)
	}
}
