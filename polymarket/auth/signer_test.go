package auth

import "testing"

func TestConfigValidateRequiresPrivateKeyAndFunderAddress(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

