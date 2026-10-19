package httptransport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/config"
	"github.com/piresc/gokit/middleware"
	"github.com/piresc/gokit/redis"
	"github.com/piresc/gokit/testkit"

	"github.com/piresc/go-project/internal/user/domain"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type routerEnv struct {
	srv     http.Handler
	signer  *auth.RSASigner
	store   *auth.RedisRevocationStore
	auth    *fakeAuth
	profile *fakeProfile
}

func newRouterEnv(t *testing.T) *routerEnv {
	t.Helper()
	fp := &fakeProfile{user: &domain.User{ID: "u1", Email: "e@x.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	e := newRouterEnvWith(t, fp, fp)
	e.profile = fp
	return e
}

func newRouterEnvWith(t *testing.T, reader ProfileReader, writer ProfileWriter) *routerEnv {
	t.Helper()
	priv, _, _ := testkit.NewRSAKeyPair(t)

	mr := miniredis.RunT(t)
	rc, err := redis.NewClient(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port})
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	store := auth.NewRedisRevocationStore(rc)

	mw := middleware.NewMiddleware(discardLogger(), nil)
	authMW := mw.RS256Auth(auth.NewRSAVerifier(&priv.PublicKey), middleware.RS256Options{Revocation: store})

	fa := &fakeAuth{pair: &domain.TokenPair{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, authMW, NewAuthHandler(fa, discardLogger()), NewUserHandler(reader, writer, discardLogger()))

	return &routerEnv{srv: mux, signer: auth.NewRSASigner(priv, "user-service", time.Minute), store: store, auth: fa}
}

func (e *routerEnv) token(t *testing.T) (string, string, time.Time) {
	t.Helper()
	tok, jti, exp, err := e.signer.Sign("u1", "e@x.com", "investor")
	if err != nil {
		t.Fatal(err)
	}
	return tok, jti, exp
}

func (e *routerEnv) do(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.srv.ServeHTTP(rr, req)
	return rr
}

var protected = []struct{ method, path, body string }{
	{http.MethodPost, "/auth/logout", ""},
	{http.MethodGet, "/users/me", ""},
	{http.MethodPatch, "/users/me", `{"display_name":"x"}`},
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	e := newRouterEnv(t)
	for _, rt := range protected {
		rr := e.do(rt.method, rt.path, rt.body, "")
		if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), `"code":"ERR_UNAUTHORIZED","message":"Authentication required"`) {
			t.Errorf("%s %s: got %d %s", rt.method, rt.path, rr.Code, rr.Body.String())
		}
	}
}

func TestProtectedRoutesRejectRevokedToken(t *testing.T) {
	e := newRouterEnv(t)
	tok, jti, _ := e.token(t)
	if err := e.store.Revoke(context.Background(), jti, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, rt := range protected {
		rr := e.do(rt.method, rt.path, rt.body, tok)
		if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), `"code":"ERR_TOKEN_REVOKED","message":"Token has been revoked"`) {
			t.Errorf("%s %s: got %d %s", rt.method, rt.path, rr.Code, rr.Body.String())
		}
	}
}

func TestLogoutRevokesTokenForSubsequentRequests(t *testing.T) {
	e := newRouterEnv(t)
	e.auth.revoke = e.store.Revoke
	tok, jti, _ := e.token(t)

	if rr := e.do(http.MethodPost, "/auth/logout", "", tok); rr.Code != http.StatusOK {
		t.Fatalf("logout: got %d %s", rr.Code, rr.Body.String())
	}
	if e.auth.logoutJTI != jti {
		t.Fatalf("logout jti = %q, want %q", e.auth.logoutJTI, jti)
	}
	rr := e.do(http.MethodGet, "/users/me", "", tok)
	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), `"code":"ERR_TOKEN_REVOKED","message":"Token has been revoked"`) {
		t.Fatalf("after logout: got %d %s", rr.Code, rr.Body.String())
	}
}

func TestProtectedRouteAcceptsValidToken(t *testing.T) {
	e := newRouterEnv(t)
	tok, _, _ := e.token(t)
	if rr := e.do(http.MethodGet, "/users/me", "", tok); rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestPublicRoutesReachableWithoutToken(t *testing.T) {
	e := newRouterEnv(t)
	for _, p := range []string{"/auth/google", "/auth/apple"} {
		if rr := e.do(http.MethodPost, p, `{"id_token":"t"}`, ""); rr.Code != http.StatusOK {
			t.Errorf("%s: got %d %s", p, rr.Code, rr.Body.String())
		}
	}
	if rr := e.do(http.MethodPost, "/auth/refresh", `{"refresh_token":"t"}`, ""); rr.Code != http.StatusOK {
		t.Errorf("refresh: got %d", rr.Code)
	}
}

func TestUnknownUserPathIs404(t *testing.T) {
	e := newRouterEnv(t)
	if rr := e.do(http.MethodGet, "/users/unknown-path", "", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("got %d", rr.Code)
	}
}
