package rtds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClient_SubscribeCrypto_BTC_UsesStringFilter(t *testing.T) {
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

	c, err := NewClient(Config{
		URL: "ws" + srv.URL[len("http"):],
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.SubscribeCrypto(ctx, "BTC"); err != nil {
		t.Fatalf("SubscribeCrypto: %v", err)
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
	if got["type"] != "subscribe" || got["channel"] != "crypto" {
		t.Fatalf("unexpected subscribe envelope: %v", got)
	}
	if got["filters"] != `{"symbol":"btcusdt"}` {
		t.Fatalf("expected filters JSON string, got %T %v", got["filters"], got["filters"])
	}
}

