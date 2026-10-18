package usecase

import "errors"

var (
	// ErrNotImplemented is returned by placeholder behavior.
	ErrNotImplemented = errors.New("not implemented")
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidInput is returned when a caller-supplied value is malformed.
	ErrInvalidInput = errors.New("invalid input")
)
