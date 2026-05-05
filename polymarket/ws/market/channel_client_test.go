package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
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
	if msg.MessageType != "" {
		t.Fatalf("expected empty message type for control frame, got %q", msg.MessageType)
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

func TestChannelClient_ReadMessage_IgnoresRawPONGHeartbeat(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		if err := c.WriteMessage(websocket.TextMessage, []byte("PONG")); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
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

	msg, err := c.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.MessageType != "" {
		t.Fatalf("MessageType = %q, want empty for raw heartbeat", msg.MessageType)
	}
	if string(msg.Raw) != "PONG" {
		t.Fatalf("Raw = %q, want PONG", string(msg.Raw))
	}
}

func TestChannelClient_KeepaliveSendsPolymarketPING(t *testing.T) {
	t.Parallel()

	pingCh := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		for {
			mt, payload, err := c.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage && string(payload) == "PING" {
				pingCh <- append([]byte(nil), payload...)
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewChannelClient(Config{
		URL:          "ws" + srv.URL[len("http"):],
		PingInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewChannelClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case got := <-pingCh:
		if string(got) != "PING" {
			t.Fatalf("keepalive payload = %q, want PING", string(got))
		}
	case <-ctx.Done():
		t.Fatalf("expected Polymarket text PING before timeout: %v", ctx.Err())
	}
}

func TestChannelClient_ReadMessage_ReturnsTypedDecodeErrorForUnknownExplicitBatch(t *testing.T) {
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
				"event_type":"mystery_event",
				"asset_id":"token-no",
				"timestamp":"1700000000100"
			},
			{
				"event_type":"mystery_event_2",
				"asset_id":"token-yes",
				"timestamp":"1700000000200"
			}
		]`)
		if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
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

	_, err = c.ReadMessage(ctx)
	if err == nil {
		t.Fatal("ReadMessage() error = nil, want typed decode error")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("ReadMessage() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "market.decode" {
		t.Fatalf("expected market.decode op, got %q", typed.Op)
	}
	if !strings.Contains(string(typed.RawBody), `"event_type":"mystery_event"`) {
		t.Fatalf("expected raw body for unknown item, got %q", string(typed.RawBody))
	}
}

func TestChannelClient_ReadMessage_ReturnsTypedDecodeErrorForFallbackAndUnknownExplicitBatch(t *testing.T) {
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
				"event":"book",
				"assets_ids":["token_yes"]
			},
			{
				"event_type":"mystery_event",
				"asset_id":"token-no",
				"timestamp":"1700000000100"
			}
		]`)
		if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
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

	_, err = c.ReadMessage(ctx)
	if err == nil {
		t.Fatal("ReadMessage() error = nil, want typed decode error")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("ReadMessage() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "market.decode" {
		t.Fatalf("expected market.decode op, got %q", typed.Op)
	}
	if !strings.Contains(string(typed.RawBody), `"event_type":"mystery_event"`) {
		t.Fatalf("expected raw body for unknown item, got %q", string(typed.RawBody))
	}
}
