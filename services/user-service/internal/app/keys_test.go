package app

import (
	"strings"
	"testing"

	"github.com/piresc/gokit/config"
	"github.com/piresc/gokit/testkit"
)

func TestLoadKeysEmptyConfig(t *testing.T) {
	_, _, err := loadKeys(config.AuthConfig{})
	if err == nil || !strings.Contains(err.Error(), "JWT_PRIVATE_KEY_PEM") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadKeysOK(t *testing.T) {
	_, privPEM, pubPEM := testkit.NewRSAKeyPair(t)
	priv, pub, err := loadKeys(config.AuthConfig{PrivateKeyPEM: string(privPEM), PublicKeyPEM: string(pubPEM)})
	if err != nil || priv == nil || pub == nil {
		t.Fatalf("got %v %v %v", priv, pub, err)
	}
}

func TestLoadKeysMissingPublic(t *testing.T) {
	_, privPEM, _ := testkit.NewRSAKeyPair(t)
	_, _, err := loadKeys(config.AuthConfig{PrivateKeyPEM: string(privPEM)})
	if err == nil || !strings.Contains(err.Error(), "JWT_PUBLIC_KEY_PEM") {
		t.Fatalf("got %v", err)
	}
}
