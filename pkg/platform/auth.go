package platform

import (
	"fmt"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/middleware"
)

// keyConfigured reports whether a public key is set in config or environment.
func (a *App[S]) keyConfigured() bool {
	c := a.Config.Auth
	return c.PublicKeyPEM != "" || c.PublicKeyPath != ""
}

// initAuth builds Revocations from the service's own Redis and, when a public
// key is configured, the Auth and OptionalAuth middleware. A configured key
// without Redis is an error because tokens could not be checked for
// revocation, unless service.auth.skip_revocation_check is true: then
// Revocations stays nil and revoked tokens are accepted until they expire.
func (a *App[S]) initAuth(name string) error {
	skip := a.Settings.Auth.SkipRevocationCheck
	if !skip && a.Redis != nil {
		a.Revocations = auth.NewRedisRevocationStore(a.Redis)
	}
	if !a.keyConfigured() {
		return nil
	}
	if !skip && a.Revocations == nil {
		return fmt.Errorf("%s requires %s: %s (needed for token revocation checks)", name, Redis, Redis.hint())
	}
	pub, err := auth.LoadRSAPublicKey(a.Config.Auth.PublicKeyPEM, a.Config.Auth.PublicKeyPath)
	if err != nil {
		return fmt.Errorf("load public key: %w", err)
	}
	verifier := auth.NewRSAVerifier(pub)
	opts := middleware.RS256Options{FailOpen: a.Settings.Auth.FailOpen}
	if !skip {
		// Left unset when skipping: a nil *RedisRevocationStore in the
		// interface would not disable the check.
		opts.Revocation = a.Revocations
	}
	a.Auth = a.Middleware.RS256Auth(verifier, opts)
	a.OptionalAuth = a.Middleware.OptionalRS256Auth(verifier, opts)
	return nil
}
