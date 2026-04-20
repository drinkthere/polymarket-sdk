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

		if err := c.WriteMessage(websocket.TextMessage, []byte("tick")); err != nil {
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
	if err := c.Subscribe(ctx, SubscribeRequest{Symbol: "BTC"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	got, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "tick" {
		t.Fatalf("unexpected read: %q", string(got))
	}

	if err := c.Unsubscribe(ctx, UnsubscribeRequest{Symbol: "BTC"}); err != nil {
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
	if sub["type"] != "subscribe" || sub["channel"] != "crypto" {
		t.Fatalf("unexpected subscribe envelope: %v", sub)
	}
	if sub["filters"] != `{"symbol":"btcusdt"}` {
		t.Fatalf("expected filters JSON string, got %T %v", sub["filters"], sub["filters"])
	}

	var unsub map[string]any
	if err := json.Unmarshal(unsubPayload, &unsub); err != nil {
		t.Fatalf("unmarshal unsubscribe: %v", err)
	}
	if unsub["type"] != "unsubscribe" || unsub["channel"] != "crypto" {
		t.Fatalf("unexpected unsubscribe envelope: %v", unsub)
	}
	if unsub["filters"] != `{"symbol":"btcusdt"}` {
		t.Fatalf("expected filters JSON string, got %T %v", unsub["filters"], unsub["filters"])
	}
}

func TestClient_SubscribeItem_Equity_UsesUppercaseSymbol(t *testing.T) {
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
	if err := c.SubscribeItem(ctx, SubscribeItemRequest{Item: "equity:spy"}); err != nil {
		t.Fatalf("SubscribeItem: %v", err)
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
	if got["type"] != "subscribe" || got["channel"] != "equity" {
		t.Fatalf("unexpected subscribe envelope: %v", got)
	}
	if got["filters"] != `{"symbol":"SPY"}` {
		t.Fatalf("expected filters JSON string, got %T %v", got["filters"], got["filters"])
	}
}

func TestClient_ReadMessage_InfersType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		// Wait for the client to subscribe first, then emit a JSON message.
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"update","symbol":"btcusdt","timestamp":1}`))

		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
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
	if err := c.SubscribeCrypto(ctx, "BTC"); err != nil {
		t.Fatalf("SubscribeCrypto: %v", err)
	}

	msg, err := c.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.MessageType != "update" {
		t.Fatalf("expected message type update, got %q", msg.MessageType)
	}
	if len(msg.Raw) == 0 {
		t.Fatal("expected raw payload")
	}
}
