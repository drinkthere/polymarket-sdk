package orders

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if typed.Op != "orders.new" {
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

func TestClientCreateOrDeriveAPIKeyDelegatesToAuth(t *testing.T) {
	var createCalls, deriveCalls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/api-key":
			createCalls++
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/auth/derive-api-key":
			deriveCalls++
			_, _ = io.WriteString(w, `{"apiKey":"key-1","secret":"secret-1","passphrase":"pass-1"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	creds, err := client.CreateOrDeriveAPIKey(t.Context(), 0)
	if err != nil {
		t.Fatalf("CreateOrDeriveAPIKey() error = %v", err)
	}
	if creds.Key != "key-1" || creds.Secret != "secret-1" || creds.Passphrase != "pass-1" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if createCalls != 1 || deriveCalls != 1 {
		t.Fatalf("unexpected auth calls: create=%d derive=%d", createCalls, deriveCalls)
	}
}

func TestGetOpenOrdersPaginatesAndSignsRequests(t *testing.T) {
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/orders" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("POLY_API_KEY"); got != "key-1" {
			t.Fatalf("POLY_API_KEY = %q", got)
		}
		if got := r.Header.Get("POLY_SIGNATURE"); got == "" {
			t.Fatal("expected POLY_SIGNATURE header")
		}
		if got := r.URL.Query().Get("market"); got != "market-1" {
			t.Fatalf("market = %q", got)
		}

		calls++
		switch calls {
		case 1:
			if got := r.URL.Query().Get("next_cursor"); got != "MA==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"ord-1","status":"LIVE","owner":"owner-1","maker_address":"maker-1","market":"market-1","asset_id":"asset-1","side":"BUY","original_size":"5","size_matched":"1","price":"0.42","associate_trades":["trade-1"],"outcome":"Yes","created_at":1,"expiration":"10","order_type":"GTC"}],"next_cursor":"MQ=="}`)
		case 2:
			if got := r.URL.Query().Get("next_cursor"); got != "MQ==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"ord-2","status":"LIVE","owner":"owner-1","maker_address":"maker-1","market":"market-1","asset_id":"asset-2","side":"SELL","original_size":"3","size_matched":"0","price":"0.55","associate_trades":[],"outcome":"No","created_at":2,"expiration":"11","order_type":"GTC"}],"next_cursor":"LTE="}`)
		default:
			t.Fatalf("unexpected call count: %d", calls)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	got, err := client.GetOpenOrders(t.Context(), GetOpenOrdersRequest{
		Credentials: validCreds(),
		Market:      "market-1",
	})
	if err != nil {
		t.Fatalf("GetOpenOrders() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(GetOpenOrders()) = %d, want 2", len(got))
	}
	if got[0].ID != "ord-1" || got[1].ID != "ord-2" {
		t.Fatalf("unexpected open orders: %+v", got)
	}
}

func TestGetUserTradesPaginatesWithFiltersAndDecodesFullPayload(t *testing.T) {
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/trades" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("POLY_API_KEY"); got != "key-1" {
			t.Fatalf("POLY_API_KEY = %q", got)
		}
		if got := r.Header.Get("POLY_SIGNATURE"); got == "" {
			t.Fatal("expected POLY_SIGNATURE header")
		}
		for key, want := range map[string]string{
			"id":            "trade-filter",
			"maker_address": "0x1234567890123456789012345678901234567890",
			"market":        "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"asset_id":      "asset-1",
			"before":        "200",
			"after":         "100",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("%s = %q", key, got)
			}
		}
		calls++
		switch calls {
		case 1:
			if got := r.URL.Query().Get("next_cursor"); got != "MA==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-1","taker_order_id":"take-1","asset_id":"asset-1","market":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","side":"BUY","price":"0.41","size":"5","fee_rate_bps":"30","status":"MATCHED","match_time":"1","last_update":"11","created_at":"1","owner":"owner","outcome":"YES","bucket_index":7,"maker_address":"0x1234567890123456789012345678901234567890","transaction_hash":"0xabc","trader_side":"TAKER","maker_orders":[{"order_id":"maker-1","owner":"maker-owner","maker_address":"0x9999999999999999999999999999999999999999","matched_amount":"3","price":"0.41","fee_rate_bps":"0","asset_id":"asset-1","outcome":"YES","side":"SELL"}]}],"next_cursor":"MQ=="}`)
		case 2:
			if got := r.URL.Query().Get("next_cursor"); got != "MQ==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-2","taker_order_id":"take-2","asset_id":"asset-2","market":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","side":"SELL","price":"0.52","size":"2","fee_rate_bps":"15","status":"CONFIRMED","match_time":"2","last_update":"12","created_at":"2","owner":"owner-2","outcome":"NO","bucket_index":8,"maker_address":"0x1234567890123456789012345678901234567890","transaction_hash":"0xdef","trader_side":"MAKER","maker_orders":[{"order_id":"maker-2","owner":"maker-owner-2","maker_address":"0x8888888888888888888888888888888888888888","matched_amount":"2","price":"0.52","fee_rate_bps":"5","asset_id":"asset-2","outcome":"NO","side":"BUY"}]}],"next_cursor":"LTE="}`)
		default:
			t.Fatalf("unexpected page %d", calls)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	trades, err := client.GetUserTrades(t.Context(), GetUserTradesRequest{
		Credentials:  validCreds(),
		ID:           "trade-filter",
		MakerAddress: "0x1234567890123456789012345678901234567890",
		Market:       "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AssetID:      "asset-1",
		Before:       "200",
		After:        "100",
	})
	if err != nil {
		t.Fatalf("GetUserTrades() error = %v", err)
	}
	if len(trades) != 2 || trades[0].ID != "tr-1" || trades[1].ID != "tr-2" {
		t.Fatalf("unexpected trades = %+v", trades)
	}
	if trades[0].TakerOrderID != "take-1" || trades[0].FeeRateBPS != "30" || trades[0].LastUpdate != "11" {
		t.Fatalf("unexpected first trade metadata = %+v", trades[0])
	}
	if trades[0].Outcome != "YES" || trades[0].BucketIndex != 7 || trades[0].MakerAddress != "0x1234567890123456789012345678901234567890" {
		t.Fatalf("unexpected first trade outcome fields = %+v", trades[0])
	}
	if trades[0].TransactionHash != "0xabc" || trades[0].TraderSide != "TAKER" {
		t.Fatalf("unexpected first trade transaction fields = %+v", trades[0])
	}
	if len(trades[0].MakerOrders) != 1 {
		t.Fatalf("expected 1 maker order, got %+v", trades[0].MakerOrders)
	}
	if got := trades[0].MakerOrders[0]; got.OrderID != "maker-1" || got.Owner != "maker-owner" || got.MakerAddress != "0x9999999999999999999999999999999999999999" || got.MatchedAmount != "3" || got.Price != "0.41" || got.FeeRateBPS != "0" || got.AssetID != "asset-1" || got.Outcome != "YES" || got.Side != "SELL" {
		t.Fatalf("unexpected maker order = %+v", got)
	}
	if trades[1].TakerOrderID != "take-2" || trades[1].FeeRateBPS != "15" || trades[1].LastUpdate != "12" || trades[1].Outcome != "NO" || trades[1].BucketIndex != 8 || trades[1].TransactionHash != "0xdef" || trades[1].TraderSide != "MAKER" {
		t.Fatalf("unexpected second trade = %+v", trades[1])
	}
}

func TestGetUserTradesRawPaginatesWithFilters(t *testing.T) {
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/trades" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
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
		for key, want := range map[string]string{
			"id":            "trade-filter",
			"maker_address": "0x1234567890123456789012345678901234567890",
			"market":        "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"asset_id":      "asset-1",
			"before":        "200",
			"after":         "100",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("%s = %q", key, got)
			}
		}
		calls++
		switch calls {
		case 1:
			if got := r.URL.Query().Get("next_cursor"); got != "MA==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-1","taker_order_id":"take-1"}],"next_cursor":"MQ=="}`)
		case 2:
			if got := r.URL.Query().Get("next_cursor"); got != "MQ==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-2","taker_order_id":"take-2"}],"next_cursor":"LTE="}`)
		default:
			t.Fatalf("unexpected page %d", calls)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	raw, err := client.GetUserTradesRaw(t.Context(), GetUserTradesRawRequest{
		Credentials:  validCreds(),
		ID:           "trade-filter",
		MakerAddress: "0x1234567890123456789012345678901234567890",
		Market:       "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AssetID:      " asset-1 ",
		Before:       "200",
		After:        "100",
	})
	if err != nil {
		t.Fatalf("GetUserTradesRaw() error = %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("len(raw) = %d", len(raw))
	}
	if string(raw[0]) != `{"id":"tr-1","taker_order_id":"take-1"}` {
		t.Fatalf("unexpected raw[0] = %s", string(raw[0]))
	}
	if string(raw[1]) != `{"id":"tr-2","taker_order_id":"take-2"}` {
		t.Fatalf("unexpected raw[1] = %s", string(raw[1]))
	}
}

func TestGetUserTradesRawReturnsTypedAuthError(t *testing.T) {
	client := newTestHTTPClient(t)

	ordersClient, err := NewClient(client, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = ordersClient.GetUserTradesRaw(t.Context(), GetUserTradesRawRequest{
		Credentials: polyauth.APICredentials{
			Key:        "key-1",
			Secret:     "!!!",
			Passphrase: "pass-1",
		},
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
	if typed.Op != "orders.get_user_trades_raw" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestGetUserTradesRawReturnsTypedDecodeError(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not-json`)
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetUserTradesRaw(t.Context(), GetUserTradesRawRequest{
		Credentials: validCreds(),
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
	if typed.Op != "orders.get_user_trades_raw" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
	if string(typed.RawBody) != "not-json" {
		t.Fatalf("unexpected raw body: %q", string(typed.RawBody))
	}
}

func TestGetUserTradesFailsOnRepeatedCursor(t *testing.T) {
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/trades" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		calls++
		switch calls {
		case 1:
			if got := r.URL.Query().Get("next_cursor"); got != "MA==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-1"}],"next_cursor":"MQ=="}`)
		case 2:
			if got := r.URL.Query().Get("next_cursor"); got != "MQ==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-2"}],"next_cursor":"MQ=="}`)
		default:
			t.Fatalf("unexpected extra page %d", calls)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetUserTrades(t.Context(), GetUserTradesRequest{Credentials: validCreds()})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrProtocol {
		t.Fatalf("expected ErrProtocol, got %v", typed.Kind)
	}
	if typed.Op != "orders.get_user_trades" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
	if !strings.Contains(typed.Message, "repeated next_cursor") {
		t.Fatalf("unexpected message: %q", typed.Message)
	}
}

func TestGetUserTradesRawFailsOnRepeatedCursor(t *testing.T) {
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/trades" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		calls++
		switch calls {
		case 1:
			if got := r.URL.Query().Get("next_cursor"); got != "MA==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-1"}],"next_cursor":"MQ=="}`)
		case 2:
			if got := r.URL.Query().Get("next_cursor"); got != "MQ==" {
				t.Fatalf("next_cursor = %q", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"tr-2"}],"next_cursor":"MQ=="}`)
		default:
			t.Fatalf("unexpected extra page %d", calls)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetUserTradesRaw(t.Context(), GetUserTradesRawRequest{Credentials: validCreds()})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrProtocol {
		t.Fatalf("expected ErrProtocol, got %v", typed.Kind)
	}
	if typed.Op != "orders.get_user_trades_raw" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
	if !strings.Contains(typed.Message, "repeated next_cursor") {
		t.Fatalf("unexpected message: %q", typed.Message)
	}
}

func TestGetUserTradesRawAllowsMoreThanThousandPages(t *testing.T) {
	const totalPages = 1001
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/trades" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		calls++

		wantCursor := "MA=="
		if calls > 1 {
			wantCursor = fmt.Sprintf("cursor-%d", calls-1)
		}
		if got := r.URL.Query().Get("next_cursor"); got != wantCursor {
			t.Fatalf("next_cursor = %q, want %q", got, wantCursor)
		}

		nextCursor := "LTE="
		if calls < totalPages {
			nextCursor = fmt.Sprintf("cursor-%d", calls)
		}
		_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[{"id":"tr-%d"}],"next_cursor":"%s"}`, calls, nextCursor))
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	raw, err := client.GetUserTradesRaw(t.Context(), GetUserTradesRawRequest{
		Credentials: validCreds(),
	})
	if err != nil {
		t.Fatalf("GetUserTradesRaw() error = %v", err)
	}
	if calls != totalPages {
		t.Fatalf("calls = %d, want %d", calls, totalPages)
	}
	if len(raw) != totalPages {
		t.Fatalf("len(raw) = %d, want %d", len(raw), totalPages)
	}
}

func TestGetUserTradesAllowsMoreThanThousandPages(t *testing.T) {
	const totalPages = 1001
	var calls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/data/trades" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		calls++

		wantCursor := "MA=="
		if calls > 1 {
			wantCursor = fmt.Sprintf("cursor-%d", calls-1)
		}
		if got := r.URL.Query().Get("next_cursor"); got != wantCursor {
			t.Fatalf("next_cursor = %q, want %q", got, wantCursor)
		}

		nextCursor := "LTE="
		if calls < totalPages {
			nextCursor = fmt.Sprintf("cursor-%d", calls)
		}
		_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[{"id":"tr-%d"}],"next_cursor":"%s"}`, calls, nextCursor))
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	trades, err := client.GetUserTrades(t.Context(), GetUserTradesRequest{
		Credentials: validCreds(),
	})
	if err != nil {
		t.Fatalf("GetUserTrades() error = %v", err)
	}
	if calls != totalPages {
		t.Fatalf("calls = %d, want %d", calls, totalPages)
	}
	if len(trades) != totalPages {
		t.Fatalf("len(trades) = %d, want %d", len(trades), totalPages)
	}
}

func TestPlaceMakerOrderPostsTypedPayload(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/order" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		text := string(body)
		for _, needle := range []string{
			`"owner":"key-1"`,
			`"orderType":"GTC"`,
			`"tokenId":"asset-1"`,
			`"timestamp":"1713398400000"`,
			`"metadata":"0x0000000000000000000000000000000000000000000000000000000000000000"`,
			`"builder":"0x0000000000000000000000000000000000000000000000000000000000000000"`,
			`"signature":"0xsig"`,
		} {
			if !strings.Contains(text, needle) {
				t.Fatalf("request body missing %s: %s", needle, text)
			}
		}
		if got := r.Header.Get("POLY_PASSPHRASE"); got != "pass-1" {
			t.Fatalf("POLY_PASSPHRASE = %q", got)
		}
		_, _ = io.WriteString(w, `{"success":true,"errorMsg":"","orderID":"ord-1","transactionsHashes":["0xtx"],"status":"LIVE","takingAmount":"21","makingAmount":"50"}`)
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	got, err := client.PlaceMakerOrder(t.Context(), PlaceMakerOrderRequest{
		Credentials: validCreds(),
		Owner:       "key-1",
		OrderType:   OrderTypeGTC,
		Order: MakerOrder{
			Salt:          1,
			Maker:         "0xmaker",
			Signer:        "0xsigner",
			TokenID:       "asset-1",
			MakerAmount:   "50",
			TakerAmount:   "21",
			Side:          SideBuy,
			Expiration:    "9999999999",
			SignatureType: 0,
			Timestamp:     "1713398400000",
			Metadata:      "0x0000000000000000000000000000000000000000000000000000000000000000",
			Builder:       "0x0000000000000000000000000000000000000000000000000000000000000000",
			Signature:     "0xsig",
		},
	})
	if err != nil {
		t.Fatalf("PlaceMakerOrder() error = %v", err)
	}
	if !got.Success || got.OrderID != "ord-1" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestPlaceMakerOrderRespectsPostOnlyFalse(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/order" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if !strings.Contains(string(body), `"postOnly":false`) {
			t.Fatalf("request body missing postOnly=false: %s", string(body))
		}
		_, _ = io.WriteString(w, `{"success":true,"errorMsg":"","orderID":"ord-2","transactionsHashes":[],"status":"LIVE","takingAmount":"21","makingAmount":"50"}`)
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.PlaceMakerOrder(t.Context(), PlaceMakerOrderRequest{
		Credentials: validCreds(),
		Owner:       "key-1",
		OrderType:   OrderTypeGTC,
		PostOnly:    false,
		Order: MakerOrder{
			Salt:          1,
			Maker:         "0xmaker",
			Signer:        "0xsigner",
			TokenID:       "asset-1",
			MakerAmount:   "50",
			TakerAmount:   "21",
			Side:          SideBuy,
			Expiration:    "9999999999",
			SignatureType: 0,
			Timestamp:     "1713398400000",
			Metadata:      "0x0000000000000000000000000000000000000000000000000000000000000000",
			Builder:       "0x0000000000000000000000000000000000000000000000000000000000000000",
			Signature:     "0xsig",
		},
	})
	if err != nil {
		t.Fatalf("PlaceMakerOrder() error = %v", err)
	}
}

func TestCancelOrderAndCancelAllOrders(t *testing.T) {
	var cancelOrderCalls, cancelAllCalls int

	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/order":
			cancelOrderCalls++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if string(body) != `{"orderID":"ord-1"}` {
				t.Fatalf("unexpected cancel body: %s", string(body))
			}
			_, _ = io.WriteString(w, `{"canceled":["ord-1"]}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/cancel-all":
			cancelAllCalls++
			_, _ = io.WriteString(w, `{"canceled":["ord-1","ord-2"]}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	client, err := NewClient(httpClient, validAuthConfig())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	cancelOne, err := client.CancelOrder(t.Context(), CancelOrderRequest{
		Credentials: validCreds(),
		OrderID:     "ord-1",
	})
	if err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	if len(cancelOne.Canceled) != 1 || cancelOne.Canceled[0] != "ord-1" {
		t.Fatalf("unexpected cancel response: %+v", cancelOne)
	}

	cancelAll, err := client.CancelAllOrders(t.Context(), CancelAllOrdersRequest{
		Credentials: validCreds(),
	})
	if err != nil {
		t.Fatalf("CancelAllOrders() error = %v", err)
	}
	if len(cancelAll.Canceled) != 2 {
		t.Fatalf("unexpected cancel-all response: %+v", cancelAll)
	}
	if cancelOrderCalls != 1 || cancelAllCalls != 1 {
		t.Fatalf("unexpected call counts: cancelOrder=%d cancelAll=%d", cancelOrderCalls, cancelAllCalls)
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
