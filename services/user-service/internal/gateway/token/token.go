// Package token issues access and refresh tokens.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/piresc/gokit/auth"

	"github.com/piresc/go-project/services/user-service/internal/domain"
	"github.com/piresc/go-project/services/user-service/internal/usecase"
)

var _ usecase.TokenService = (*Service)(nil)

// Service signs access tokens with a gokit RSA signer and mints refresh tokens.
type Service struct {
	signer     *auth.RSASigner
	refreshTTL time.Duration
}

// NewService creates a token service.
func NewService(signer *auth.RSASigner, refreshTTL time.Duration) *Service {
	return &Service{signer: signer, refreshTTL: refreshTTL}
}

// GenerateAccessToken signs an RS256 access token for the user.
func (s *Service) GenerateAccessToken(user *domain.User) (string, string, time.Time, error) {
	return s.signer.Sign(user.ID, user.Email, user.Role)
}

// GenerateRefreshToken returns a random plaintext token (for the client), its
// SHA-256 hex hash (for storage) and its expiry.
func (s *Service) GenerateRefreshToken() (string, string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", time.Time{}, fmt.Errorf("generating random bytes: %w", err)
	}
	plaintext := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, hex.EncodeToString(sum[:]), time.Now().Add(s.refreshTTL), nil
}
