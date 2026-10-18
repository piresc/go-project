package oauth

import "github.com/piresc/go-project/services/user-service/internal/usecase"

const appleJWKSURL = "https://appleid.apple.com/auth/keys"

var _ usecase.OAuthVerifier = (*AppleVerifier)(nil)

// AppleVerifier verifies Apple ID tokens using Apple's JWKS endpoint. Apple may
// omit email/name on repeat sign-ins, so those identity fields can be empty.
type AppleVerifier struct {
	jwksVerifier
}

// NewAppleVerifier creates a verifier for the given (comma-separated) client IDs.
func NewAppleVerifier(clientID string) *AppleVerifier {
	return &AppleVerifier{jwksVerifier: newJWKSVerifier("apple", appleJWKSURL, clientID)}
}
