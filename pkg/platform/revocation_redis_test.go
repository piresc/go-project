package platform

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/piresc/gokit/auth"
)

// ownRedisApp writes a config with its own Redis, a public key and authYAML,
// applies env, and builds the app. It returns the app, the signing key and
// the service's own miniredis.
func ownRedisApp(t *testing.T, authYAML string, env map[string]string) (*App[struct{}], *rsa.PrivateKey, *miniredis.Miniredis) {
	t.Helper()
	priv, _, pubPEM := newKeyPair(t)
	keyPath := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(keyPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	own := newMiniredis(t)
	writeConfig(t, redisYAML(own)+"auth:\n  public_key_path: "+keyPath+"\n"+authYAML)
	for k, v := range env {
		t.Setenv(k, v)
	}
	a, err := New[struct{}]("svc", Require(Redis, AuthKeys))
	if err != nil {
		t.Fatal(err)
	}
	return a, priv, own
}

func authedHandler(a *App[struct{}]) http.Handler {
	return a.Auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
}

func signToken(t *testing.T, priv *rsa.PrivateKey) (token, jti string) {
	t.Helper()
	token, jti, _, err := auth.NewRSASigner(priv, "test", time.Hour).Sign("u-1", "a@b.c", "investor")
	if err != nil {
		t.Fatal(err)
	}
	return token, jti
}

func revocationYAML(host string, port int) string {
	return fmt.Sprintf("service:\n  auth:\n    revocation_redis:\n      host: %q\n      port: %d\n", host, port)
}

func TestAuth_RevocationRedis(t *testing.T) {
	rev := newMiniredis(t)
	a, priv, own := ownRedisApp(t, revocationYAML(rev.Host(), rev.Server().Addr().Port), nil)
	h := authedHandler(a)
	ctx := context.Background()

	// Revoked through the revocation Redis: rejected.
	tok, jti := signToken(t, priv)
	if w := serve(t, h, tok); w.Code != http.StatusOK {
		t.Fatalf("valid token: status %d", w.Code)
	}
	if err := a.Revocations.Revoke(ctx, jti, time.Hour); err != nil {
		t.Fatal(err)
	}
	if w := serve(t, h, tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked in revocation redis: status %d", w.Code)
	}
	if len(rev.Keys()) == 0 || len(own.Keys()) != 0 {
		t.Fatalf("keys: revocation redis %v, own redis %v", rev.Keys(), own.Keys())
	}

	// Same kind of write to the service's own Redis: not consulted.
	tok2, jti2 := signToken(t, priv)
	if err := auth.NewRedisRevocationStore(a.Redis).Revoke(ctx, jti2, time.Hour); err != nil {
		t.Fatal(err)
	}
	if w := serve(t, h, tok2); w.Code != http.StatusOK {
		t.Fatalf("revoked in own redis only: status %d, want 200", w.Code)
	}
}

// TestAuth_RevocationRedisEnvHost shows SERVICE_AUTH_REVOCATION_REDIS_HOST
// overrides the YAML host, provided the key is present in the YAML.
func TestAuth_RevocationRedisEnvHost(t *testing.T) {
	rev := newMiniredis(t)
	a, priv, _ := ownRedisApp(t, revocationYAML("", rev.Server().Addr().Port),
		map[string]string{"SERVICE_AUTH_REVOCATION_REDIS_HOST": rev.Host()})
	if a.revocationRedis == nil {
		t.Fatal("env host did not enable the revocation redis")
	}
	tok, jti := signToken(t, priv)
	if err := a.Revocations.Revoke(context.Background(), jti, time.Hour); err != nil {
		t.Fatal(err)
	}
	if w := serve(t, authedHandler(a), tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
	if len(rev.Keys()) == 0 {
		t.Fatal("revocation did not land in the env-configured redis")
	}
}

func TestAuth_RevocationRedisUnreachable(t *testing.T) {
	dead := newMiniredis(t)
	host, port := dead.Host(), dead.Server().Addr().Port
	dead.Close()

	_, _, pubPEM := newKeyPair(t)
	keyPath := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(keyPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, redisYAML(newMiniredis(t))+"auth:\n  public_key_path: "+keyPath+"\n"+revocationYAML(host, port))
	_, err := New[struct{}]("svc", Require(Redis, AuthKeys))
	if err == nil || !strings.Contains(err.Error(), "revocation redis") {
		t.Fatalf("got %v", err)
	}
}

func TestAuth_RevocationRedisUnsetUsesOwnRedis(t *testing.T) {
	a, priv, own := ownRedisApp(t, "", nil)
	if a.revocationRedis != nil {
		t.Fatal("revocationRedis must be nil when unset")
	}
	tok, jti := signToken(t, priv)
	if err := a.Revocations.Revoke(context.Background(), jti, time.Hour); err != nil {
		t.Fatal(err)
	}
	if w := serve(t, authedHandler(a), tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
	if len(own.Keys()) == 0 {
		t.Fatal("revocation did not land in the own redis")
	}
}

func TestShutdown_ClosesRevocationRedis(t *testing.T) {
	rev := newMiniredis(t)
	a, _, _ := ownRedisApp(t, revocationYAML(rev.Host(), rev.Server().Addr().Port), nil)
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.revocationRedis.GetClient().Ping(context.Background()).Err(); err == nil {
		t.Fatal("revocation redis still open after Shutdown")
	}
}
