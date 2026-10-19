package usecase

import "errors"

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidInput is returned when a caller-supplied value is malformed.
	ErrInvalidInput = errors.New("invalid input")
	// ErrTooManyHoldings is returned when a portfolio is at its holding limit.
	ErrTooManyHoldings = errors.New("too many holdings")
	// ErrPricesUnavailable is returned when quotes cannot be fetched.
	ErrPricesUnavailable = errors.New("prices unavailable")
)
