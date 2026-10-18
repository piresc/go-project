package usecase

import "errors"

var (
	ErrInvalidToken    = errors.New("invalid token")
	ErrTokenExpired    = errors.New("token expired")
	ErrInvalidAudience = errors.New("invalid token audience")
	ErrRefreshExpired  = errors.New("refresh token expired")
	ErrInvalidRefresh  = errors.New("invalid refresh token")
	ErrUserNotFound    = errors.New("user not found")
	ErrInvalidInput    = errors.New("invalid input")
)
