package auth

import (
	"fmt"
	"strings"
)

type Config struct {
	FunderAddress string
	PrivateKey    string
	ChainID       int
	SignatureType int
	APIKey        string
	APISecret     string
	APIPassphrase string
}

func (c Config) APICredentials() APICredentials {
	return APICredentials{
		Key:        c.APIKey,
		Secret:     c.APISecret,
		Passphrase: c.APIPassphrase,
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.FunderAddress) == "" {
		return fmt.Errorf("funder_address is required")
	}
	if strings.TrimSpace(c.PrivateKey) == "" {
		return fmt.Errorf("private_key is required")
	}
	return nil
}
