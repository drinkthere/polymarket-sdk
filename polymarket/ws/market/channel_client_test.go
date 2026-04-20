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

func TestChannelClient_Subscribe_Unsubscribe_ReadMessage(t *testing.T) {
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

		// Send an application message to be read via ReadMessage().
		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"event":"book","assets_ids":["token_yes"]}`)); err != nil {
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

	c, err := NewChannelClient(Config{URL: "ws" + srv.URL[len("http"):]})
	if err != nil {
		t.Fatalf("NewChannelClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.Subscribe(ctx, ChannelSubscribeRequest{AssetIDs: []string{"token_yes"}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	msg, err := c.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.MessageType != "book" {
		t.Fatalf("expected message type book, got %q", msg.MessageType)
	}

	if err := c.Unsubscribe(ctx, ChannelUnsubscribeRequest{AssetIDs: []string{"token_yes"}}); err != nil {
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
	if sub["operation"] != "subscribe" {
		t.Fatalf("unexpected subscribe operation: %v", sub)
	}
	if sub["custom_feature_enabled"] != true {
		t.Fatalf("expected custom_feature_enabled=true, got %v", sub["custom_feature_enabled"])
	}
	ids, ok := sub["assets_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "token_yes" {
		t.Fatalf("unexpected assets_ids: %T %v", sub["assets_ids"], sub["assets_ids"])
	}

	var unsub map[string]any
	if err := json.Unmarshal(unsubPayload, &unsub); err != nil {
		t.Fatalf("unmarshal unsubscribe: %v", err)
	}
	if unsub["operation"] != "unsubscribe" {
		t.Fatalf("unexpected unsubscribe operation: %v", unsub)
	}
	// Custom feature flag must not be required on unsubscribe.
	if _, ok := unsub["custom_feature_enabled"]; ok {
		t.Fatalf("expected custom_feature_enabled omitted on unsubscribe, got %v", unsub["custom_feature_enabled"])
	}
}
