package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

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

func TestNewSignerBuildsStableHeaders(t *testing.T) {
	cfg := Config{
		FunderAddress: "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       80002,
	}

	signer, err := NewSigner(cfg)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	if got := signer.Address(); got != cfg.FunderAddress {
		t.Fatalf("Address() = %q, want %q", got, cfg.FunderAddress)
	}

	l1, err := signer.CreateL1Headers(time.Unix(1700000000, 0), 3)
	if err != nil {
		t.Fatalf("CreateL1Headers() error = %v", err)
	}
	if l1.POLY_ADDRESS != cfg.FunderAddress {
		t.Fatalf("POLY_ADDRESS = %q, want %q", l1.POLY_ADDRESS, cfg.FunderAddress)
	}
	if l1.POLY_TIMESTAMP != "1700000000" {
		t.Fatalf("POLY_TIMESTAMP = %q", l1.POLY_TIMESTAMP)
	}
	if l1.POLY_NONCE != "3" {
		t.Fatalf("POLY_NONCE = %q", l1.POLY_NONCE)
	}
	if l1.POLY_SIGNATURE == "" {
		t.Fatal("expected non-empty POLY_SIGNATURE")
	}

	l2, err := signer.CreateL2Headers(APICredentials{
		Key:        "key-1",
		Secret:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Passphrase: "pass-1",
	}, L2HeaderArgs{
		Method:      "test-sign",
		RequestPath: "/orders",
		Body:        `{"hash": "0x123"}`,
	}, time.Unix(1000000, 0))
	if err != nil {
		t.Fatalf("CreateL2Headers() error = %v", err)
	}
	if l2.POLY_SIGNATURE != "ZwAdJKvoYRlEKDkNMwd5BuwNNtg93kNaR_oU2HrfVvc=" {
		t.Fatalf("POLY_SIGNATURE = %q", l2.POLY_SIGNATURE)
	}
	if l2.POLY_API_KEY != "key-1" {
		t.Fatalf("POLY_API_KEY = %q", l2.POLY_API_KEY)
	}
	if l2.POLY_PASSPHRASE != "pass-1" {
		t.Fatalf("POLY_PASSPHRASE = %q", l2.POLY_PASSPHRASE)
	}
}

func TestCreateOrDeriveAPIKeyFallsBackToDerive(t *testing.T) {
	var createCalls, deriveCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/api-key":
			createCalls++
			if got := r.Header.Get("POLY_ADDRESS"); got == "" {
				t.Fatalf("missing POLY_ADDRESS header")
			}
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/auth/derive-api-key":
			deriveCalls++
			_, _ = w.Write([]byte(`{"apiKey":"key-1","secret":"secret-1","passphrase":"pass-1"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New() error = %v", err)
	}

	client, err := NewClient(httpClient, Config{
		FunderAddress: "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       80002,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	got, err := client.CreateOrDeriveAPIKey(context.Background(), 0)
	if err != nil {
		t.Fatalf("CreateOrDeriveAPIKey() error = %v", err)
	}
	if got.Key != "key-1" || got.Secret != "secret-1" || got.Passphrase != "pass-1" {
		t.Fatalf("unexpected creds: %+v", got)
	}
	if createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", createCalls)
	}
	if deriveCalls != 1 {
		t.Fatalf("deriveCalls = %d, want 1", deriveCalls)
	}
}
