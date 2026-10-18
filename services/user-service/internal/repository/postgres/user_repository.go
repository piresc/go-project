// Package postgres implements the user-service persistence ports on Postgres.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/piresc/go-project/services/user-service/internal/domain"
	"github.com/piresc/go-project/services/user-service/internal/usecase"
)

var _ usecase.UserRepository = (*UserRepository)(nil)

// UserRepository is the Postgres implementation of usecase.UserRepository.
type UserRepository struct {
	db *sqlx.DB
}

// NewUserRepository creates a Postgres-backed user repository.
func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{db: db}
}

const userColumns = `u.id, u.email, COALESCE(u.display_name, '') AS display_name,
		COALESCE(u.avatar_url, '') AS avatar_url, u.role, u.created_at, u.updated_at`

// FindByID returns a user by ID, or (nil, nil) if not found.
func (r *UserRepository) FindByID(ctx context.Context, id string) (*domain.User, error) {
	return r.getUser(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id)
}

// FindByEmail returns a user by email, or (nil, nil) if not found.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.getUser(ctx, `SELECT `+userColumns+` FROM users u WHERE u.email = $1`, email)
}

// FindByProvider returns a user by OAuth provider identity, or (nil, nil) if not found.
func (r *UserRepository) FindByProvider(ctx context.Context, provider, subject string) (*domain.User, error) {
	return r.getUser(ctx,
		`SELECT `+userColumns+`
		 FROM users u
		 JOIN auth_providers ap ON u.id = ap.user_id
		 WHERE ap.provider = $1 AND ap.provider_subject = $2`, provider, subject)
}

func (r *UserRepository) getUser(ctx context.Context, query string, args ...any) (*domain.User, error) {
	var user domain.User
	if err := r.db.GetContext(ctx, &user, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// Create inserts a new user row.
func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, avatar_url, role, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		user.ID, user.Email, user.DisplayName, user.AvatarURL, user.Role, user.CreatedAt, user.UpdatedAt)
	return err
}

// UpdateDisplayName updates a user's display name.
func (r *UserRepository) UpdateDisplayName(ctx context.Context, id, name string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET display_name = $1, updated_at = NOW() WHERE id = $2`, name, id)
	return err
}

// CreateAuthProvider inserts an auth provider link.
func (r *UserRepository) CreateAuthProvider(ctx context.Context, provider *domain.AuthProvider) error {
	if provider.ID == "" {
		provider.ID = uuid.NewString()
	}
	if provider.CreatedAt.IsZero() {
		provider.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO auth_providers (id, user_id, provider, provider_subject, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		provider.ID, provider.UserID, provider.Provider, provider.ProviderSubject, provider.CreatedAt)
	return err
}

// CreateRefreshToken inserts a refresh token row.
func (r *UserRepository) CreateRefreshToken(ctx context.Context, token *domain.RefreshToken) error {
	if token.ID == "" {
		token.ID = uuid.NewString()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		token.ID, token.UserID, token.TokenHash, token.ExpiresAt, token.Revoked, token.CreatedAt)
	return err
}

// FindRefreshTokenByHash returns a refresh token by hash, or (nil, nil) if not found.
func (r *UserRepository) FindRefreshTokenByHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	var token domain.RefreshToken
	err := r.db.GetContext(ctx, &token,
		`SELECT id, user_id, token_hash, expires_at, revoked, created_at
		 FROM refresh_tokens
		 WHERE token_hash = $1`, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &token, nil
}

// RevokeRefreshToken atomically revokes a single refresh token. It returns
// usecase.ErrInvalidRefresh when the token is unknown or already revoked, so
// concurrent rotations of the same token cannot both succeed.
func (r *UserRepository) RevokeRefreshToken(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = TRUE WHERE id = $1 AND revoked = FALSE`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return usecase.ErrInvalidRefresh
	}
	return nil
}

// RevokeAllUserRefreshTokens revokes all refresh tokens for a user.
func (r *UserRepository) RevokeAllUserRefreshTokens(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND revoked = FALSE`, userID)
	return err
}
