package token

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/testkit"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

func TestGenerateRefreshToken(t *testing.T) {
	svc := NewService(nil, 30*24*time.Hour)

	plain, hash, exp, err := svc.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if len(plain) != 64 {
		t.Fatalf("plaintext length = %d, want 64 hex chars", len(plain))
	}
	sum := sha256.Sum256([]byte(plain))
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash is not sha256(plaintext)")
	}
	if d := time.Until(exp); d < 29*24*time.Hour || d > 30*24*time.Hour {
		t.Fatalf("unexpected expiry %v", exp)
	}

	plain2, hash2, _, err := svc.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("second GenerateRefreshToken: %v", err)
	}
	if plain == plain2 || hash == hash2 {
		t.Fatal("two calls returned identical tokens")
	}
}

func TestGenerateAccessToken(t *testing.T) {
	priv, _, _ := testkit.NewRSAKeyPair(t)
	signer := auth.NewRSASigner(priv, "test-issuer", 15*time.Minute)
	svc := NewService(signer, time.Hour)

	user := &domain.User{ID: "user-1", Email: "a@example.com", Role: "investor"}
	tok, jti, exp, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	if jti == "" || !exp.After(time.Now()) {
		t.Fatalf("bad jti/exp: %q %v", jti, exp)
	}

	claims, err := auth.NewRSAVerifier(&priv.PublicKey).Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-1" || claims.Email != "a@example.com" || claims.Role != "investor" || claims.ID != jti {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

// The dev seed (Task 5) stores this hash for the plaintext "seed-refresh-token".
func TestSeedRefreshTokenHash(t *testing.T) {
	const want = "94ddae8dc4a371cbdcc42ca2635623330272847ec68495b572d155d9bae902f0"
	sum := sha256.Sum256([]byte("seed-refresh-token"))
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("seed hash = %s, want %s", got, want)
	}
}
