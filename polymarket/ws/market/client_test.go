package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClient_SubscribeMarket_IncludesAssetsIDs(t *testing.T) {
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
	if err := c.SubscribeMarket(ctx, []int64{101, 202, 303}); err != nil {
		t.Fatalf("SubscribeMarket: %v", err)
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
	if got["type"] != "subscribe" || got["channel"] != "market" {
		t.Fatalf("unexpected subscribe envelope: %v", got)
	}
	ids, ok := got["assets_ids"].([]any)
	if !ok {
		t.Fatalf("expected assets_ids array, got %T %v", got["assets_ids"], got["assets_ids"])
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 assets_ids, got %v", ids)
	}
}

