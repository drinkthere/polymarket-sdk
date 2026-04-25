package balances

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"
	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

func TestNewClientRequiresHTTPTransport(t *testing.T) {
	_, err := NewClient(nil, polyauth.Config{
		FunderAddress: "0x1111111111111111111111111111111111111111",
		PrivateKey:    "0xabc123",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrRequestBuild {
		t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
	}
	if typed.Op != "balances.new" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestNewClientValidatesAuthConfig(t *testing.T) {
	httpClient := newTestHTTPClient(t)

	_, err := NewClient(httpClient, polyauth.Config{})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrAuth {
		t.Fatalf("expected ErrAuth, got %v", typed.Kind)
	}
	if typed.Message != "funder_address is required" {
		t.Fatalf("unexpected message: %q", typed.Message)
	}
}

func TestNewClientReturnsClientForValidInputs(t *testing.T) {
	httpClient := newTestHTTPClient(t)

	client, err := NewClient(httpClient, polyauth.Config{
		FunderAddress: "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       80002,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func TestGetBalanceSignsRequest(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/balance-allowance" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("POLY_API_KEY"); got != "key-1" {
			t.Fatalf("POLY_API_KEY = %q", got)
		}
		if got := r.URL.Query().Get("asset_type"); got != "COLLATERAL" {
			t.Fatalf("asset_type = %q", got)
		}
		if got := r.URL.Query().Get("signature_type"); got != "0" {
			t.Fatalf("signature_type = %q", got)
		}
		_, _ = io.WriteString(w, `{"balance":"12.34","allowance":"56.78"}`)
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	got, err := client.GetBalance(t.Context(), GetBalanceRequest{
		Credentials:   validCreds(),
		AssetType:     AssetTypeCollateral,
		SignatureType: 0,
	})
	if err != nil {
		t.Fatalf("GetBalance() error = %v", err)
	}
	if got.Balance != "12.34" || got.Allowance != "56.78" {
		t.Fatalf("unexpected balance response: %+v", got)
	}
}

func newTestHTTPClient(t *testing.T) *httpx.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(server.Close)

	client, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}
	return client
}

func newHTTPClientWithServer(t *testing.T, handler http.Handler) *httpx.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}
	return client
}

func validAuthConfig() polyauth.Config {
	return polyauth.Config{
		FunderAddress: "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       80002,
	}
}

func validCreds() polyauth.APICredentials {
	return polyauth.APICredentials{
		Key:        "key-1",
		Secret:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Passphrase: "pass-1",
	}
}
