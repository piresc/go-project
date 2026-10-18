package oauth

import "github.com/piresc/go-project/internal/user/usecase"

// Verification failures are reported directly as the use-case sentinels, so
// callers can match them with errors.Is without any translation layer.
var (
	// errInvalidToken is returned when the ID token cannot be verified.
	errInvalidToken = usecase.ErrInvalidToken
	// errTokenExpired is returned when the ID token is expired.
	errTokenExpired = usecase.ErrTokenExpired
	// errInvalidAudience is returned when the aud claim matches no configured client ID.
	errInvalidAudience = usecase.ErrInvalidAudience
)
