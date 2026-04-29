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

func TestClient_Subscribe_Unsubscribe_Read(t *testing.T) {
	t.Parallel()

	subMsgCh := make(chan []byte, 1)
	unsubMsgCh := make(chan []byte, 1)

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
			t.Errorf("ReadMessage(sub): %v", err)
			return
		}
		subMsgCh <- payload

		if err := c.WriteMessage(websocket.TextMessage, []byte("book")); err != nil {
			t.Errorf("WriteMessage: %v", err)
			return
		}

		_, payload, err = c.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage(unsub): %v", err)
			return
		}
		unsubMsgCh <- payload
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
	if err := c.Subscribe(ctx, SubscribeRequest{AssetIDs: []int64{101, 202, 303}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	got, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "book" {
		t.Fatalf("unexpected read: %q", string(got))
	}

	if err := c.Unsubscribe(ctx, UnsubscribeRequest{AssetIDs: []int64{101, 202, 303}}); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	var subPayload, unsubPayload []byte
	select {
	case subPayload = <-subMsgCh:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for subscribe: %v", ctx.Err())
	}
	select {
	case unsubPayload = <-unsubMsgCh:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for unsubscribe: %v", ctx.Err())
	}

	var sub map[string]any
	if err := json.Unmarshal(subPayload, &sub); err != nil {
		t.Fatalf("unmarshal subscribe: %v", err)
	}
	if sub["type"] != "subscribe" || sub["channel"] != "market" {
		t.Fatalf("unexpected subscribe envelope: %v", sub)
	}

	var unsub map[string]any
	if err := json.Unmarshal(unsubPayload, &unsub); err != nil {
		t.Fatalf("unmarshal unsubscribe: %v", err)
	}
	if unsub["type"] != "unsubscribe" || unsub["channel"] != "market" {
		t.Fatalf("unexpected unsubscribe envelope: %v", unsub)
	}
}

func TestClient_ReadMessage_IgnoresControlFrameEventFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"event":"book","assets_ids":["token_yes"]}`)); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
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

	msg, err := c.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.MessageType != "" {
		t.Fatalf("MessageType = %q, want empty for control frame", msg.MessageType)
	}
}

func TestClient_ReadMessage_UsesEmptyTypeForMixedBatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		payload := []byte(`[
			{
				"event_type":"book",
				"asset_id":"token-yes",
				"bids":[{"price":"0.48","size":"30"}],
				"asks":[{"price":"0.52","size":"25"}],
				"timestamp":"1700000000000"
			},
			{
				"event_type":"best_bid_ask",
				"asset_id":"token-yes",
				"best_bid":"0.48",
				"best_ask":"0.52",
				"timestamp":"1700000000100"
			}
		]`)
		if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
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

	msg, err := c.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.MessageType != "" {
		t.Fatalf("MessageType = %q, want empty for mixed batch", msg.MessageType)
	}
}
