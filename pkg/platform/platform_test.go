package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

const baseYAML = `app:
  name: svc
  environment: local
  version: v0.0.1
server:
  port: 0
  shutdown_timeout: 1
logger:
  level: error
  format: text
tracing:
  enabled: false
  sample_rate: 0.0
`

var errBoom = errors.New("boom")

// writeConfig writes config/local/svc.yaml (baseYAML plus extra) into a temp
// dir and changes into it. Env vars that could override the file are cleared.
func writeConfig(t *testing.T, extra string) {
	t.Helper()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config", "local")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "svc.yaml"), []byte(baseYAML+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("APP_ENV", "local")
	for _, k := range []string{"DB_HOST", "REDIS_HOST", "REDIS_PORT", "NATS_URL", "JWT_PUBLIC_KEY_PEM", "JWT_PUBLIC_KEY_PATH"} {
		t.Setenv(k, "")
	}
}

func redisYAML(mr *miniredis.Miniredis) string {
	return fmt.Sprintf("redis:\n  host: %s\n  port: %d\n", mr.Host(), mr.Server().Addr().Port)
}

func newMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	return mr
}

func TestNew_RequireMissing(t *testing.T) {
	writeConfig(t, "")
	_, err := New[struct{}]("svc", Require(Postgres))
	if err == nil || err.Error() != "svc requires postgres: set database.host" {
		t.Fatalf("got %v", err)
	}
}

func TestNew_ServiceConfigDecoded(t *testing.T) {
	writeConfig(t, "service:\n  greeting: hi\n")
	a, err := New[struct {
		Greeting string `mapstructure:"greeting"`
	}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	if a.Service.Greeting != "hi" {
		t.Fatalf("greeting = %q", a.Service.Greeting)
	}
}

func TestNew_AuthNilWithoutKey(t *testing.T) {
	writeConfig(t, "")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	if a.Auth != nil || a.OptionalAuth != nil {
		t.Fatal("Auth/OptionalAuth must be nil without a public key")
	}
}

func TestNew_AuthNeedsRedis(t *testing.T) {
	_, _, pubPEM := newKeyPair(t)
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "public.pem")
	if err := os.WriteFile(keyPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "auth:\n  public_key_path: "+keyPath+"\n")
	_, err := New[struct{}]("svc")
	if err == nil || !strings.Contains(err.Error(), "redis") {
		t.Fatalf("got %v", err)
	}
}

func TestRun_StartErrorStopsBeforeServe(t *testing.T) {
	writeConfig(t, "")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	a.onStop(func(context.Context) error { stopped = true; return nil })
	a.onStart(func(context.Context) error { return errBoom })

	if err := a.Run(); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if !stopped {
		t.Fatal("stop funcs did not run")
	}
}

func TestRun_StartFuncsRunInOrder(t *testing.T) {
	writeConfig(t, "")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	a.onStart(func(context.Context) error { order = append(order, "first"); return nil })
	a.onStart(func(context.Context) error { order = append(order, "second"); return errBoom })
	a.onStart(func(context.Context) error { order = append(order, "third"); return nil })

	if err := a.Run(); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("order = %v", order)
	}
}

func TestRun_ShutdownOrder(t *testing.T) {
	mr := newMiniredis(t)
	writeConfig(t, redisYAML(mr))
	a, err := New[struct{}]("svc", Require(Redis))
	if err != nil {
		t.Fatal(err)
	}
	var pingErr error
	ran := false
	a.onStop(func(ctx context.Context) error {
		ran = true
		pingErr = a.Redis.GetClient().Ping(ctx).Err()
		return nil
	})

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("stop func did not run")
	}
	if pingErr != nil {
		t.Fatalf("redis already closed when stop func ran: %v", pingErr)
	}
}
