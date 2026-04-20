package auth

import (
	"fmt"
	"strings"
)

type Config struct {
	FunderAddress string
	PrivateKey    string
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

