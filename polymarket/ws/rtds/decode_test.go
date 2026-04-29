package rtds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/gorilla/websocket"
)

func TestDecodeUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   []byte
		check func(t *testing.T, got Update)
	}{
		{
			name: "crypto update",
			raw: []byte(`{
				"topic":"crypto_prices",
				"type":"update",
				"timestamp":1753314088421,
				"payload":{
					"symbol":"btcusdt",
					"timestamp":1753314088395,
					"value":67234.5
				}
			}`),
			check: func(t *testing.T, got Update) {
				t.Helper()
				if got.CryptoPrice == nil || got.CryptoPrice.Symbol != "btcusdt" || got.CryptoPrice.Value != 67234.5 {
					t.Fatalf("unexpected crypto update: %+v", got)
				}
			},
		},
		{
			name: "chainlink update",
			raw: []byte(`{
				"topic":"crypto_prices_chainlink",
				"type":"update",
				"timestamp":1753314064237,
				"payload":{
					"symbol":"eth/usd",
					"timestamp":1753314064213,
					"value":3456.78
				}
			}`),
			check: func(t *testing.T, got Update) {
				t.Helper()
				if got.ChainlinkPrice == nil || got.ChainlinkPrice.Symbol != "eth/usd" || got.ChainlinkPrice.Timestamp != 1753314064213 {
					t.Fatalf("unexpected chainlink update: %+v", got)
				}
			},
		},
		{
			name: "equity snapshot",
			raw: []byte(`{
				"topic":"equity_prices",
				"type":"subscribe",
				"timestamp":1711382400000,
				"payload":{
					"symbol":"aapl",
					"data":[
						{"timestamp":1711382280000,"value":198.30},
						{"timestamp":1711382340000,"value":198.41}
					]
				}
			}`),
			check: func(t *testing.T, got Update) {
				t.Helper()
				if got.EquitySnapshot == nil || len(got.EquitySnapshot.Data) != 2 {
					t.Fatalf("unexpected equity snapshot: %+v", got)
				}
				if got.EquitySnapshot.Symbol != "aapl" || got.EquitySnapshot.Data[1].Value != 198.41 {
					t.Fatalf("unexpected equity snapshot payload: %+v", got.EquitySnapshot)
				}
			},
		},
		{
			name: "equity live update",
			raw: []byte(`{
				"topic":"equity_prices",
				"type":"update",
				"timestamp":1711400000000,
				"payload":{
					"symbol":"xauusd",
					"value":2175.30,
					"full_accuracy_value":"2175.3012",
					"timestamp":1711399000000,
					"received_at":1711400000002,
					"is_carried_forward":true
				}
			}`),
			check: func(t *testing.T, got Update) {
				t.Helper()
				if got.EquityPrice == nil || got.EquityPrice.Symbol != "xauusd" {
					t.Fatalf("unexpected equity price update: %+v", got)
				}
				if !got.EquityPrice.IsCarriedForward || got.EquityPrice.FullAccuracyValue != "2175.3012" {
					t.Fatalf("unexpected equity price payload: %+v", got.EquityPrice)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecodeUpdate(tt.raw)
			if err != nil {
				t.Fatalf("DecodeUpdate() error = %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestDecodeUpdateReturnsTypedDecodeError(t *testing.T) {
	t.Parallel()

	_, err := DecodeUpdate([]byte(`{"topic":"crypto_prices","payload":"oops"}`))
	if err == nil {
		t.Fatal("DecodeUpdate() error = nil, want typed decode error")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("DecodeUpdate() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "rtds.decode_crypto_price" {
		t.Fatalf("expected decode_crypto_price op, got %q", typed.Op)
	}
	if len(typed.RawBody) == 0 {
		t.Fatal("expected raw body on typed decode error")
	}
}

func TestClient_SubscribeChainlink_UsesLowercaseFilter(t *testing.T) {
	t.Parallel()

	msgCh := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		_, payload, err := c.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage: %v", err)
			return
		}
		msgCh <- payload
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Config{URL: "ws" + srv.URL[len("http"):]})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.SubscribeChainlink(ctx, "ETH/USD"); err != nil {
		t.Fatalf("SubscribeChainlink: %v", err)
	}

	var payload []byte
	select {
	case payload = <-msgCh:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for subscribe: %v", ctx.Err())
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["action"] != "subscribe" {
		t.Fatalf("unexpected subscribe action: %v", got)
	}

	subscriptions, ok := got["subscriptions"].([]any)
	if !ok || len(subscriptions) != 1 {
		t.Fatalf("expected exactly one subscription, got %T %v", got["subscriptions"], got["subscriptions"])
	}
	sub, ok := subscriptions[0].(map[string]any)
	if !ok {
		t.Fatalf("expected subscription object, got %T", subscriptions[0])
	}
	if sub["topic"] != "crypto_prices_chainlink" || sub["type"] != "*" {
		t.Fatalf("unexpected chainlink subscription: %v", sub)
	}
	if sub["filters"] != `{"symbol":"eth/usd"}` {
		t.Fatalf("expected lowercase chainlink filter JSON string, got %T %v", sub["filters"], sub["filters"])
	}
}
