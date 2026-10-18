package oauth

import (
	"context"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/piresc/go-project/services/user-service/internal/domain"
	"github.com/piresc/go-project/services/user-service/internal/usecase"
)

const testAppleClientID = "com.example.app"

func setupAppleTestVerifier(t *testing.T) (*AppleVerifier, *rsa.PrivateKey, *jwksTestServer) {
	t.Helper()
	privKey := newTestKey(t)
	srv := newJWKSServer(t, privKey)
	v := NewAppleVerifier(testAppleClientID)
	v.jwksURL = srv.URL
	return v, privKey, srv
}

func validAppleClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user-67890",
		"aud":   testAppleClientID,
		"email": "apple@example.com",
		"name":  "Apple User",
		"iat":   jwt.NewNumericDate(now),
		"exp":   jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

func TestAppleVerifier_ValidToken(t *testing.T) {
	v, privKey, _ := setupAppleTestVerifier(t)

	got, err := v.Verify(context.Background(), signTestToken(t, privKey, validAppleClaims()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := domain.OAuthIdentity{Subject: "apple-user-67890", Email: "apple@example.com", Name: "Apple User", Provider: "apple"}
	if *got != want {
		t.Fatalf("identity = %+v, want %+v", *got, want)
	}
}

func TestAppleVerifier_ValidTokenWithoutEmailAndName(t *testing.T) {
	v, privKey, _ := setupAppleTestVerifier(t)
	claims := validAppleClaims()
	delete(claims, "email")
	delete(claims, "name")

	got, err := v.Verify(context.Background(), signTestToken(t, privKey, claims))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "apple-user-67890" || got.Email != "" || got.Name != "" || got.Provider != "apple" {
		t.Fatalf("unexpected identity: %+v", got)
	}
}

func TestAppleVerifier_ExpiredToken(t *testing.T) {
	v, privKey, _ := setupAppleTestVerifier(t)
	claims := validAppleClaims()
	claims["exp"] = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	claims["iat"] = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))

	_, err := v.Verify(context.Background(), signTestToken(t, privKey, claims))
	if !errors.Is(err, usecase.ErrTokenExpired) {
		t.Errorf("error = %v, want ErrTokenExpired", err)
	}
}

func TestAppleVerifier_InvalidAudience(t *testing.T) {
	v, privKey, _ := setupAppleTestVerifier(t)
	claims := validAppleClaims()
	claims["aud"] = "wrong-client-id"

	_, err := v.Verify(context.Background(), signTestToken(t, privKey, claims))
	if !errors.Is(err, usecase.ErrInvalidAudience) {
		t.Errorf("error = %v, want ErrInvalidAudience", err)
	}
}

func TestAppleVerifier_AllowsAnyConfiguredAudience(t *testing.T) {
	v, privKey, _ := setupAppleTestVerifier(t)
	v.clientIDs = parseAllowedAudiences(testAppleClientID + ", com.example.web")
	claims := validAppleClaims()
	claims["aud"] = "com.example.web"

	if _, err := v.Verify(context.Background(), signTestToken(t, privKey, claims)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleVerifier_InvalidSignature(t *testing.T) {
	v, _, _ := setupAppleTestVerifier(t)
	token := signTestToken(t, newTestKey(t), validAppleClaims())

	_, err := v.Verify(context.Background(), token)
	if !errors.Is(err, usecase.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

func TestAppleVerifier_MalformedToken(t *testing.T) {
	v, _, _ := setupAppleTestVerifier(t)
	_, err := v.Verify(context.Background(), "not-a-jwt")
	if !errors.Is(err, usecase.ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

func TestAppleVerifier_JWKSCaching(t *testing.T) {
	v, privKey, srv := setupAppleTestVerifier(t)
	token := signTestToken(t, privKey, validAppleClaims())

	for i := 0; i < 2; i++ {
		if _, err := v.Verify(context.Background(), token); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Errorf("JWKS fetched %d times, want 1 (should be cached)", n)
	}
}

func TestAppleVerifier_WarmCacheAndStaleFallback(t *testing.T) {
	v, privKey, srv := setupAppleTestVerifier(t)
	if err := v.WarmCache(context.Background()); err != nil {
		t.Fatalf("WarmCache: %v", err)
	}
	v.mu.Lock()
	v.fetchedAt = time.Now().Add(-2 * time.Hour)
	v.mu.Unlock()
	srv.available.Store(false)

	got, err := v.Verify(context.Background(), signTestToken(t, privKey, validAppleClaims()))
	if err != nil {
		t.Fatalf("Verify with stale keys failed: %v", err)
	}
	if got.Provider != "apple" {
		t.Fatalf("provider = %q", got.Provider)
	}
}
