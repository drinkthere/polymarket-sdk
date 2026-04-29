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
		if got := r.URL.Query().Get("signature_type"); got != "2" {
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

func TestGetBalanceRequiresConditionalTokenID(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = balancesClient.GetBalance(t.Context(), GetBalanceRequest{
		Credentials:   validCreds(),
		AssetType:     AssetTypeConditional,
		SignatureType: 0,
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
	if typed.Op != "balances.get_balance" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestGetBalanceRejectsInvalidAssetTypeBeforeCredentialLookup(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = balancesClient.GetBalance(t.Context(), GetBalanceRequest{
		AssetType:     " BOGUS ",
		SignatureType: 0,
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
	if typed.Op != "balances.get_balance" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestUpdateAllowanceDefaultsAndSignsRequest(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/balance-allowance/update" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("asset_type"); got != "COLLATERAL" {
			t.Fatalf("asset_type = %q", got)
		}
		if got := r.URL.Query().Get("signature_type"); got != "2" {
			t.Fatalf("signature_type = %q", got)
		}
		if got := r.Header.Get("POLY_API_KEY"); got != "key-1" {
			t.Fatalf("POLY_API_KEY = %q", got)
		}
		if got := r.Header.Get("POLY_PASSPHRASE"); got != "pass-1" {
			t.Fatalf("POLY_PASSPHRASE = %q", got)
		}
		if got := r.Header.Get("POLY_SIGNATURE"); got == "" {
			t.Fatal("expected POLY_SIGNATURE header")
		}
		if got := r.Header.Get("POLY_TIMESTAMP"); got == "" {
			t.Fatal("expected POLY_TIMESTAMP header")
		}
		if got := r.Header.Get("POLY_ADDRESS"); got != "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266" {
			t.Fatalf("POLY_ADDRESS = %q", got)
		}
		if got := r.URL.Query().Get("token_id"); got != "" {
			t.Fatalf("token_id = %q", got)
		}
		_, _ = io.WriteString(w, `{"updated":true}`)
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		Credentials:   validCreds(),
		SignatureType: 0,
	})
	if err != nil {
		t.Fatalf("UpdateAllowance() error = %v", err)
	}
	if !resp.Updated {
		t.Fatal("expected Updated=true")
	}
}

func TestUpdateAllowanceAllowsEmptySuccessBody(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		Credentials:   validCreds(),
		SignatureType: 0,
	})
	if err != nil {
		t.Fatalf("UpdateAllowance() error = %v", err)
	}
	if !resp.Updated {
		t.Fatalf("expected truthful success response, got %+v", resp)
	}
}

func TestUpdateAllowanceRequiresConditionalTokenID(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		Credentials:   validCreds(),
		SignatureType: 0,
		AssetType:     AssetTypeConditional,
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
	if typed.Op != "balances.update_allowance" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestUpdateAllowanceRejectsWhitespaceConditionalBeforeCredentialLookup(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		AssetType:     " CONDITIONAL ",
		SignatureType: 0,
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
	if typed.Op != "balances.update_allowance" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestUpdateAllowanceIncludesConditionalTokenID(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/balance-allowance/update" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("asset_type"); got != "CONDITIONAL" {
			t.Fatalf("asset_type = %q", got)
		}
		if got := r.URL.Query().Get("signature_type"); got != "2" {
			t.Fatalf("signature_type = %q", got)
		}
		if got := r.URL.Query().Get("token_id"); got != "123" {
			t.Fatalf("token_id = %q", got)
		}
		_, _ = io.WriteString(w, `{"updated":true}`)
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		Credentials:   validCreds(),
		SignatureType: 2,
		AssetType:     " CONDITIONAL ",
		TokenID:       " 123 ",
	})
	if err != nil {
		t.Fatalf("UpdateAllowance() error = %v", err)
	}
	if !resp.Updated {
		t.Fatal("expected Updated=true")
	}
}

func TestUpdateAllowanceReturnsTypedAuthError(t *testing.T) {
	client := newTestHTTPClient(t)

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		Credentials: polyauth.APICredentials{
			Key:        "key-1",
			Secret:     "!!!",
			Passphrase: "pass-1",
		},
		SignatureType: 2,
	})
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
	if typed.Op != "balances.update_allowance" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestUpdateAllowanceReturnsTypedDecodeError(t *testing.T) {
	client := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not-json`)
	}))

	balancesClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = balancesClient.UpdateAllowance(t.Context(), UpdateAllowanceRequest{
		Credentials:   validCreds(),
		SignatureType: 2,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "balances.update_allowance" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
	if string(typed.RawBody) != "not-json" {
		t.Fatalf("unexpected raw body: %q", string(typed.RawBody))
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
