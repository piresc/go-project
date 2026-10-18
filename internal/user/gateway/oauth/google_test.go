package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/piresc/go-project/internal/user/domain"
	"github.com/piresc/go-project/internal/user/usecase"
)

const (
	testClientID = "test-client-id.apps.googleusercontent.com"
	testKid      = "test-key-1"
)

var (
	_ usecase.OAuthVerifier = (*GoogleVerifier)(nil)
	_ usecase.OAuthVerifier = (*AppleVerifier)(nil)
)

// jwksServer serves the public half of privKey as a JWKS and counts fetches.
// While available is false it answers 503.
type jwksTestServer struct {
	*httptest.Server
	fetches   atomic.Int32
	available atomic.Bool
}

func newJWKSServer(t *testing.T, privKey *rsa.PrivateKey) *jwksTestServer {
	t.Helper()
	s := &jwksTestServer{}
	s.available.Store(true)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.fetches.Add(1)
		if !s.available.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksResponse{Keys: []jwkKey{rsaPublicKeyToJWK(privKey.PublicKey, testKid)}})
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	return k
}

func setupTestVerifier(t *testing.T) (*GoogleVerifier, *rsa.PrivateKey, *jwksTestServer) {
	t.Helper()
	privKey := newTestKey(t)
	srv := newJWKSServer(t, privKey)
	v := NewGoogleVerifier(testClientID)
	v.jwksURL = srv.URL
	return v, privKey, srv
}

func signTestToken(t *testing.T, privKey *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = testKid
	signed, err := token.SignedString(privKey)
	if err != nil {
		t.Fatalf("signing test token: %v", err)
	}
	return signed
}

func validGoogleClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":   "https://accounts.google.com",
		"sub":   "google-user-12345",
		"aud":   testClientID,
		"email": "test@example.com",
		"name":  "Test User",
		"iat":   jwt.NewNumericDate(now),
		"exp":   jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

func rsaPublicKeyToJWK(pub rsa.PublicKey, kid string) jwkKey {
	return jwkKey{
		Kty: "RSA",
		Use: "sig",
		Kid: kid,
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func TestGoogleVerifier_ValidToken(t *testing.T) {
	v, privKey, _ := setupTestVerifier(t)
	token := signTestToken(t, privKey, validGoogleClaims())

	got, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := domain.OAuthIdentity{Subject: "google-user-12345", Email: "test@example.com", Name: "Test User", Provider: "google"}
	if *got != want {
		t.Fatalf("identity = %+v, want %+v", *got, want)
	}
}

func TestGoogleVerifier_ExpiredToken(t *testing.T) {
	v, privKey, _ := setupTestVerifier(t)
	claims := validGoogleClaims()
	claims["exp"] = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	claims["iat"] = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))

	_, err := v.Verify(context.Background(), signTestToken(t, privKey, claims))
	if !errors.Is(err, usecase.ErrTokenExpired) {
		t.Errorf("error = %v, want ErrTokenExpired", err)
	}
}

func TestGoogleVerifier_InvalidAudience(t *testing.T) {
	v, privKey, _ := setupTestVerifier(t)
	claims := validGoogleClaims()
	claims["aud"] = "wrong-client-id"

	_, err := v.Verify(context.Background(), signTestToken(t, privKey, claims))
	if !errors.Is(err, usecase.ErrInvalidAudience) {
		t.Errorf("error = %v, want ErrInvalidAudience", err)
	}
}

func TestGoogleVerifier_AllowsAnyConfiguredAudience(t *testing.T) {
	v, privKey, _ := setupTestVerifier(t)
	v.clientIDs = parseAllowedAudiences(testClientID + ",expo-ios-client-id.apps.googleusercontent.com")
	claims := validGoogleClaims()
	claims["aud"] = "expo-ios-client-id.apps.googleusercontent.com"

	got, err := v.Verify(context.Background(), signTestToken(t, privKey, claims))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "google-user-12345" {
		t.Fatalf("unexpected subject: %q", got.Subject)
	}
}

func TestGoogleVerifier_InvalidSignature(t *testing.T) {
	v, _, _ := setupTestVerifier(t)
	token := signTestToken(t, newTestKey(t), validGoogleClaims())

	_, err := v.Verify(context.Background(), token)
	if !errors.Is(err, usecase.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

func TestGoogleVerifier_MalformedToken(t *testing.T) {
	v, _, _ := setupTestVerifier(t)
	_, err := v.Verify(context.Background(), "not-a-jwt")
	if !errors.Is(err, usecase.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

func TestGoogleVerifier_JWKSCaching(t *testing.T) {
	v, privKey, srv := setupTestVerifier(t)
	token := signTestToken(t, privKey, validGoogleClaims())

	for i := 0; i < 2; i++ {
		if _, err := v.Verify(context.Background(), token); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Errorf("JWKS fetched %d times, want 1 (should be cached)", n)
	}
}

func TestGoogleVerifier_WarmCache(t *testing.T) {
	v, privKey, srv := setupTestVerifier(t)

	if err := v.WarmCache(context.Background()); err != nil {
		t.Fatalf("WarmCache: %v", err)
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Fatalf("expected exactly 1 fetch from WarmCache, got %d", n)
	}
	// First Verify after warming must not fetch again.
	if _, err := v.Verify(context.Background(), signTestToken(t, privKey, validGoogleClaims())); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Fatalf("Verify refetched JWKS after warm-up (%d fetches)", n)
	}
}

func TestGoogleVerifier_StaleKeyFallback(t *testing.T) {
	v, privKey, srv := setupTestVerifier(t)
	if err := v.WarmCache(context.Background()); err != nil {
		t.Fatalf("WarmCache: %v", err)
	}
	// Expire the cache, then make the JWKS endpoint fail.
	v.mu.Lock()
	v.fetchedAt = time.Now().Add(-2 * time.Hour)
	v.mu.Unlock()
	srv.available.Store(false)

	got, err := v.Verify(context.Background(), signTestToken(t, privKey, validGoogleClaims()))
	if err != nil {
		t.Fatalf("Verify with stale keys failed: %v", err)
	}
	if got.Subject != "google-user-12345" {
		t.Fatalf("unexpected subject %q", got.Subject)
	}
}

func TestGoogleVerifier_ColdCacheFetchFailure(t *testing.T) {
	v, privKey, srv := setupTestVerifier(t)
	srv.available.Store(false)

	_, err := v.Verify(context.Background(), signTestToken(t, privKey, validGoogleClaims()))
	if err == nil {
		t.Fatal("expected error when JWKS is unavailable and cache is cold")
	}
	if errors.Is(err, usecase.ErrInvalidToken) {
		t.Fatalf("fetch failure must not be reported as an invalid token: %v", err)
	}
}
