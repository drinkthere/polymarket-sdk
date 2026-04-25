package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

func TestClientEnsureCredentialsFallsBackToDerive(t *testing.T) {
	t.Parallel()

	var createCalled bool
	var deriveCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/api-key":
			createCalled = true
			if got := r.Header.Get("POLY_ADDRESS"); got == "" {
				t.Fatalf("expected POLY_ADDRESS header")
			}
			if got := r.Header.Get("POLY_SIGNATURE"); got == "" {
				t.Fatalf("expected POLY_SIGNATURE header")
			}
			if got := r.Header.Get("POLY_TIMESTAMP"); got == "" {
				t.Fatalf("expected POLY_TIMESTAMP header")
			}
			if got := r.Header.Get("POLY_NONCE"); got != "0" {
				t.Fatalf("expected POLY_NONCE=0, got %q", got)
			}
			http.Error(w, `{"message":"api key already exists"}`, http.StatusConflict)
		case r.Method == http.MethodGet && r.URL.Path == "/auth/derive-api-key":
			deriveCalled = true
			_ = json.NewEncoder(w).Encode(map[string]string{
				"apiKey":     "test-api-key",
				"secret":     "c2VjcmV0",
				"passphrase": "passphrase",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient, Config{
		FunderAddress: "0x1111111111111111111111111111111111111111",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       137,
		SignatureType: 2,
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	creds, err := client.EnsureCredentials(context.Background())
	if err != nil {
		t.Fatalf("EnsureCredentials() error: %v", err)
	}
	if !createCalled {
		t.Fatal("expected create api key request")
	}
	if !deriveCalled {
		t.Fatal("expected derive api key fallback")
	}
	if creds.Key != "test-api-key" {
		t.Fatalf("unexpected api key: %q", creds.Key)
	}
	if creds.Secret != "c2VjcmV0" {
		t.Fatalf("unexpected secret: %q", creds.Secret)
	}
	if creds.Passphrase != "passphrase" {
		t.Fatalf("unexpected passphrase: %q", creds.Passphrase)
	}
}
