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
	if sub["topic"] != "crypto_prices" || sub["type"] != "update" {
		t.Fatalf("unexpected crypto subscription: %v", sub)
	}
	if sub["filters"] != `{"symbol":"btcusdt"}` {
		t.Fatalf("expected crypto filter JSON string, got %T %v", sub["filters"], sub["filters"])
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
	if sub["action"] != "subscribe" {
		t.Fatalf("unexpected subscribe action: %v", sub)
	}
	subscriptions, ok := sub["subscriptions"].([]any)
	if !ok || len(subscriptions) != 1 {
		t.Fatalf("expected exactly one subscription, got %T %v", sub["subscriptions"], sub["subscriptions"])
	}
	subscribeReq, ok := subscriptions[0].(map[string]any)
	if !ok {
		t.Fatalf("expected subscription object, got %T", subscriptions[0])
	}
	if subscribeReq["topic"] != "crypto_prices" || subscribeReq["type"] != "update" {
		t.Fatalf("unexpected subscribe request: %v", subscribeReq)
	}
	if subscribeReq["filters"] != `{"symbol":"btcusdt"}` {
		t.Fatalf("expected crypto filter JSON string, got %T %v", subscribeReq["filters"], subscribeReq["filters"])
	}

	var unsub map[string]any
	if err := json.Unmarshal(unsubPayload, &unsub); err != nil {
		t.Fatalf("unmarshal unsubscribe: %v", err)
	}
	if unsub["action"] != "unsubscribe" {
		t.Fatalf("unexpected unsubscribe action: %v", unsub)
	}
	unsubSubscriptions, ok := unsub["subscriptions"].([]any)
	if !ok || len(unsubSubscriptions) != 1 {
		t.Fatalf("expected exactly one unsubscribe subscription, got %T %v", unsub["subscriptions"], unsub["subscriptions"])
	}
	unsubscribeReq, ok := unsubSubscriptions[0].(map[string]any)
	if !ok {
		t.Fatalf("expected unsubscribe object, got %T", unsubSubscriptions[0])
	}
	if unsubscribeReq["topic"] != "crypto_prices" || unsubscribeReq["type"] != "update" {
		t.Fatalf("unexpected unsubscribe request: %v", unsubscribeReq)
	}
	if unsubscribeReq["filters"] != `{"symbol":"btcusdt"}` {
		t.Fatalf("expected crypto filter JSON string, got %T %v", unsubscribeReq["filters"], unsubscribeReq["filters"])
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
	if sub["topic"] != "equity_prices" || sub["type"] != "update" {
		t.Fatalf("unexpected equity subscription: %v", sub)
	}
	if sub["filters"] != `{"symbol":"SPY"}` {
		t.Fatalf("expected equity filter JSON string, got %T %v", sub["filters"], sub["filters"])
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
