package domain

import "time"

// Portfolio is a user's collection of holdings. One exists per user.
type Portfolio struct {
	UserID    string    `db:"user_id"`
	CreatedAt time.Time `db:"created_at"`
}
