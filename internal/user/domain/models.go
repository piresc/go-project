package domain

import "time"

// User represents a registered user.
type User struct {
	ID          string    `json:"id" db:"id"`
	Email       string    `json:"email" db:"email"`
	DisplayName string    `json:"display_name" db:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" db:"avatar_url"`
	Role        string    `json:"role" db:"role"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// AuthProvider links a user to an external OAuth provider.
type AuthProvider struct {
	ID              string    `json:"id" db:"id"`
	UserID          string    `json:"user_id" db:"user_id"`
	Provider        string    `json:"provider" db:"provider"`
	ProviderSubject string    `json:"provider_subject" db:"provider_subject"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

// RefreshToken represents a stored refresh token record.
type RefreshToken struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	Revoked   bool      `json:"revoked" db:"revoked"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ProfileUpdate contains the mutable user fields.
type ProfileUpdate struct {
	DisplayName *string `json:"display_name"`
}

// TokenPair is the access and refresh token pair returned to clients.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// OAuthIdentity captures the verified claims needed by the auth use case.
type OAuthIdentity struct {
	Subject  string
	Email    string
	Name     string
	Provider string
}

// UserCreatedEvent is published to downstream services when a user is created.
type UserCreatedEvent struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}
