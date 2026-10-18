package oauth

import "github.com/piresc/go-project/services/user-service/internal/usecase"

const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

var _ usecase.OAuthVerifier = (*GoogleVerifier)(nil)

// GoogleVerifier verifies Google ID tokens using Google's JWKS endpoint.
// Verify and WarmCache come from the embedded verifier.
type GoogleVerifier struct {
	jwksVerifier
}

// NewGoogleVerifier creates a verifier for the given (comma-separated) client IDs.
func NewGoogleVerifier(clientID string) *GoogleVerifier {
	return &GoogleVerifier{jwksVerifier: newJWKSVerifier("google", googleJWKSURL, clientID)}
}
