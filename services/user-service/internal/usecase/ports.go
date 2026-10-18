package usecase

import (
	"context"
	"time"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

// UserRepository persists users, auth providers, and refresh tokens.
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*domain.User, error)
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	Create(ctx context.Context, user *domain.User) error
	UpdateDisplayName(ctx context.Context, id, name string) error
	FindByProvider(ctx context.Context, provider, subject string) (*domain.User, error)
	CreateAuthProvider(ctx context.Context, provider *domain.AuthProvider) error
	CreateRefreshToken(ctx context.Context, token *domain.RefreshToken) error
	FindRefreshTokenByHash(ctx context.Context, hash string) (*domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id string) error
	RevokeAllUserRefreshTokens(ctx context.Context, userID string) error
}

// UserCache stores user profiles.
type UserCache interface {
	GetUser(ctx context.Context, userID string) (*domain.User, bool, error)
	SetUser(ctx context.Context, user *domain.User) error
	DeleteUser(ctx context.Context, userID string) error
}

// TokenRevoker revokes access tokens by JTI. Satisfied by *auth.RedisRevocationStore.
type TokenRevoker interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
}

// EventPublisher publishes user lifecycle events.
type EventPublisher interface {
	PublishUserCreated(ctx context.Context, event domain.UserCreatedEvent) error
}

// OAuthVerifier verifies provider ID tokens.
type OAuthVerifier interface {
	Verify(ctx context.Context, idToken string) (*domain.OAuthIdentity, error)
}

// TokenService generates access and refresh tokens.
type TokenService interface {
	GenerateAccessToken(user *domain.User) (token string, jti string, exp time.Time, err error)
	GenerateRefreshToken() (plaintext string, hash string, exp time.Time, err error)
}
