package balances

import (
	"errors"
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
		FunderAddress: "0x1111111111111111111111111111111111111111",
		PrivateKey:    "0xabc123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
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
