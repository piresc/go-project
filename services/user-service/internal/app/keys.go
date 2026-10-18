package app

import (
	"crypto/rsa"
	"fmt"

	"github.com/piresc/gokit/auth"
	"github.com/piresc/gokit/config"
)

// loadKeys loads the RSA key pair used to sign and verify access tokens.
func loadKeys(cfg config.AuthConfig) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	priv, err := auth.LoadRSAPrivateKey(cfg.PrivateKeyPEM, cfg.PrivateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load JWT private key (JWT_PRIVATE_KEY_PEM or JWT_PRIVATE_KEY_PATH): %w", err)
	}
	pub, err := auth.LoadRSAPublicKey(cfg.PublicKeyPEM, cfg.PublicKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load JWT public key (JWT_PUBLIC_KEY_PEM or JWT_PUBLIC_KEY_PATH): %w", err)
	}
	return priv, pub, nil
}
