// Package oauth verifies Google and Apple ID tokens against the providers' JWKS.
package oauth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/piresc/go-project/internal/user/domain"
)

const (
	jwksCacheTTL     = time.Hour
	jwksFetchTimeout = 5 * time.Second
)

// jwksVerifier is the provider-independent ID token verifier: it caches the
// provider's JWKS, falls back to stale keys when a refresh fails, and checks
// signature, expiry and audience.
type jwksVerifier struct {
	provider   string
	clientIDs  []string
	jwksURL    string
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func newJWKSVerifier(provider, jwksURL, clientID string) jwksVerifier {
	return jwksVerifier{
		provider:   provider,
		clientIDs:  parseAllowedAudiences(clientID),
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: jwksFetchTimeout},
		keys:       make(map[string]*rsa.PublicKey),
	}
}

// WarmCache eagerly fetches and caches JWKS keys so that the first Verify
// call does not block on a synchronous fetch.
func (v *jwksVerifier) WarmCache(ctx context.Context) error {
	_, err := v.fetchKeys(ctx)
	return err
}

// Verify validates an ID token and returns the extracted identity. Claims the
// provider omits (e.g. Apple's email/name on repeat sign-ins) are empty strings.
func (v *jwksVerifier) Verify(ctx context.Context, idToken string) (*domain.OAuthIdentity, error) {
	keys, err := v.getKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}

	token, err := jwt.Parse(idToken, keyFunc(keys), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errTokenExpired
		}
		return nil, errInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !token.Valid || !ok {
		return nil, errInvalidToken
	}

	aud, err := claims.GetAudience()
	if err != nil || !containsAnyAudience(aud, v.clientIDs) {
		return nil, errInvalidAudience
	}

	return &domain.OAuthIdentity{
		Subject:  stringClaim(claims, "sub"),
		Email:    stringClaim(claims, "email"),
		Name:     stringClaim(claims, "name"),
		Provider: v.provider,
	}, nil
}

// keyFunc resolves signing keys by kid from the given JWKS key set.
func keyFunc(keys map[string]*rsa.PublicKey) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errInvalidToken
		}
		key, exists := keys[kid]
		if !exists {
			return nil, errInvalidToken
		}
		return key, nil
	}
}

// getKeys returns cached JWKS keys, fetching fresh ones if the cache is expired.
func (v *jwksVerifier) getKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	v.mu.RLock()
	if len(v.keys) > 0 && time.Since(v.fetchedAt) < jwksCacheTTL {
		keys := v.keys
		v.mu.RUnlock()
		return keys, nil
	}
	v.mu.RUnlock()

	return v.fetchKeys(ctx)
}

// fetchKeys retrieves the JWKS and caches the parsed RSA public keys. If the
// fetch fails and stale keys exist, the stale keys are returned.
func (v *jwksVerifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring the write lock.
	if len(v.keys) > 0 && time.Since(v.fetchedAt) < jwksCacheTTL {
		return v.keys, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating JWKS request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		if len(v.keys) > 0 {
			return v.keys, nil
		}
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if len(v.keys) > 0 {
			return v.keys, nil
		}
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decoding JWKS response: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}
		pub, err := parseRSAPublicKey(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("no valid RSA keys found in JWKS")
	}

	v.keys = keys
	v.fetchedAt = time.Now()
	return keys, nil
}
