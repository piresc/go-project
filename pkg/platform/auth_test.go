package platform

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/testkit"
)

func newKeyPair(t *testing.T) (*rsa.PrivateKey, []byte, []byte) {
	t.Helper()
	return testkit.NewRSAKeyPair(t)
}

func serve(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAuth_RejectsRevoked(t *testing.T) {
	priv, _, pubPEM := newKeyPair(t)
	keyPath := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(keyPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr)+"auth:\n  public_key_path: "+keyPath+"\n")

	a, err := New[struct{}]("svc", Require(Redis, AuthKeys))
	if err != nil {
		t.Fatal(err)
	}
	if a.Auth == nil || a.OptionalAuth == nil || a.Revocations == nil {
		t.Fatal("Auth, OptionalAuth and Revocations must be set")
	}

	tok, jti, _, err := auth.NewRSASigner(priv, "test", time.Hour).Sign("u-1", "a@b.c", "investor")
	if err != nil {
		t.Fatal(err)
	}
	h := a.Auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	if w := serve(t, h, tok); w.Code != http.StatusOK {
		t.Fatalf("valid token: status %d", w.Code)
	}

	if err := a.Revocations.Revoke(context.Background(), jti, time.Hour); err != nil {
		t.Fatal(err)
	}
	w := serve(t, h, tok)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked: status %d", w.Code)
	}
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Success || body.Error.Code != "ERR_TOKEN_REVOKED" {
		t.Fatalf("body = %s", w.Body.String())
	}

	mr.Close()
	w = serve(t, h, tok)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("redis down: status %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q", got)
	}
}

func writeKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, _, pubPEM := newKeyPair(t)
	keyPath := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(keyPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return priv, keyPath
}

func TestAuth_SkipRevocationCheck(t *testing.T) {
	priv, keyPath := writeKey(t)
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr)+"auth:\n  public_key_path: "+keyPath+"\nservice:\n  auth:\n    skip_revocation_check: true\n")

	a, err := New[struct{}]("svc", Require(Redis, AuthKeys))
	if err != nil {
		t.Fatal(err)
	}
	if a.Revocations != nil {
		t.Fatal("Revocations must be nil when skip_revocation_check is true")
	}
	if a.Auth == nil || a.OptionalAuth == nil {
		t.Fatal("Auth and OptionalAuth must be set")
	}

	tok, jti, _, err := auth.NewRSASigner(priv, "test", time.Hour).Sign("u-1", "a@b.c", "investor")
	if err != nil {
		t.Fatal(err)
	}
	// Revoke the JTI in the service's own Redis: it must not be consulted.
	if err := auth.NewRedisRevocationStore(a.Redis).Revoke(context.Background(), jti, time.Hour); err != nil {
		t.Fatal(err)
	}
	h := a.Auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	if w := serve(t, h, tok); w.Code != http.StatusOK {
		t.Fatalf("revoked token with check skipped: status %d, want 200", w.Code)
	}
}

func TestAuth_SkipRevocationCheckWithoutRedis(t *testing.T) {
	_, keyPath := writeKey(t)
	writeConfig(t, "auth:\n  public_key_path: "+keyPath+"\nservice:\n  auth:\n    skip_revocation_check: true\n")

	a, err := New[struct{}]("svc", Require(AuthKeys))
	if err != nil {
		t.Fatal(err)
	}
	if a.Auth == nil || a.Revocations != nil {
		t.Fatalf("Auth set=%v, Revocations=%v", a.Auth != nil, a.Revocations)
	}
}
