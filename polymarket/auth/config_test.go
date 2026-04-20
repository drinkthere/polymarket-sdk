package auth

import "testing"

func TestConfigValidateMissingFunderAddress(t *testing.T) {
	cfg := Config{PrivateKey: "0xabc"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "funder_address is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigValidateMissingPrivateKey(t *testing.T) {
	cfg := Config{FunderAddress: "0xabc"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "private_key is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigValidateBothMissingReturnsFunderAddressFirst(t *testing.T) {
	cfg := Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "funder_address is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

