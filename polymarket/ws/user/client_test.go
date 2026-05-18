package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"
	"github.com/gorilla/websocket"
)

func TestSubscribeReplaysOnReconnect(t *testing.T) {
	var connCount int
	subscribeCh := make(chan map[string]any, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		defer c.Close()

		connCount++
		_, payload, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() error = %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		subscribeCh <- msg

		if connCount == 1 {
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"), time.Now().Add(time.Second))
			return
		}
		if err := c.WriteMessage(websocket.TextMessage, []byte(`[{"event_type":"order","id":"ord-1","status":"LIVE","type":"PLACEMENT"}]`)); err != nil {
			t.Fatalf("WriteMessage() error = %v", err)
		}
		<-time.After(50 * time.Millisecond)
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		URL:              "ws" + srv.URL[len("http"):],
		Reconnect:        true,
		ReconnectBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := client.Subscribe(ctx, SubscribeRequest{
		Credentials: validCreds(),
		Markets:     []string{"0xmarket-1"},
		InitialDump: true,
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	first := <-subscribeCh
	second := <-subscribeCh

	for i, got := range []map[string]any{first, second} {
		if got["type"] != "user" {
			t.Fatalf("subscribe[%d] type = %v", i, got["type"])
		}
		if got["initial_dump"] != true {
			t.Fatalf("subscribe[%d] initial_dump = %v", i, got["initial_dump"])
		}
		auth, ok := got["auth"].(map[string]any)
		if !ok {
			t.Fatalf("subscribe[%d] missing auth: %#v", i, got)
		}
		if auth["apiKey"] != "key-1" || auth["secret"] != "secret-1" || auth["passphrase"] != "pass-1" {
			t.Fatalf("subscribe[%d] auth = %#v", i, auth)
		}
	}

	msg, err := client.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if len(msg.Events) != 1 || msg.Events[0].Order == nil {
		t.Fatalf("unexpected decoded events: %+v", msg.Events)
	}
	if msg.Events[0].Order.ID != "ord-1" {
		t.Fatalf("decoded order id = %q", msg.Events[0].Order.ID)
	}
}

func TestSubscribeReplaysOnlyLatestUserSubscriptionOnReconnect(t *testing.T) {
	var connCount int
	subscribeCh := make(chan map[string]any, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		defer c.Close()

		connCount++
		readSubscribe := func() map[string]any {
			_, payload, err := c.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage() error = %v", err)
			}
			var msg map[string]any
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			return msg
		}
		if connCount == 1 {
			subscribeCh <- readSubscribe()
			subscribeCh <- readSubscribe()
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"), time.Now().Add(time.Second))
			return
		}
		subscribeCh <- readSubscribe()
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		URL:              "ws" + strings.TrimPrefix(srv.URL, "http"),
		Reconnect:        true,
		ReconnectBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	creds := polyauth.APICredentials{Key: "key", Secret: "secret", Passphrase: "pass"}
	if err := client.Subscribe(context.Background(), SubscribeRequest{Credentials: creds, Markets: []string{"m1"}}); err != nil {
		t.Fatalf("Subscribe(m1) error = %v", err)
	}
	if err := client.Subscribe(context.Background(), SubscribeRequest{Credentials: creds, Markets: []string{"m1", "m2"}}); err != nil {
		t.Fatalf("Subscribe(m1,m2) error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	var messages []map[string]any
	for len(messages) < 3 {
		select {
		case msg := <-subscribeCh:
			messages = append(messages, msg)
		case <-deadline:
			t.Fatal("timed out waiting for replayed subscription")
		}
	}
	replay := messages[2]
	markets, ok := replay["markets"].([]any)
	if !ok {
		t.Fatalf("unexpected markets payload: %#v", replay["markets"])
	}
	if got := len(markets); got != 2 {
		t.Fatalf("expected only latest cumulative subscription to replay, got markets=%#v", markets)
	}
}

func TestDecodeEventsOrderAndTrade(t *testing.T) {
	payload := []byte(`[
		{
			"asset_id":"asset-1",
			"associate_trades":["trade-1"],
			"event_type":"order",
			"id":"ord-1",
			"market":"market-1",
			"order_owner":"owner-1",
			"original_size":"10",
			"outcome":"Yes",
			"owner":"owner-1",
			"price":"0.42",
			"side":"BUY",
			"size_matched":"2",
			"status":"LIVE",
			"timestamp":"1672290687",
			"type":"UPDATE"
		},
		{
			"asset_id":"asset-1",
			"event_type":"trade",
			"id":"trade-1",
			"last_update":"2024-01-01T00:00:00Z",
			"maker_orders":[
				{
					"asset_id":"asset-1",
					"matched_amount":"2",
					"order_id":"ord-1",
					"outcome":"Yes",
					"owner":"owner-1",
					"price":"0.42"
				}
			],
			"market":"market-1",
			"owner":"owner-1",
			"price":"0.42",
			"side":"BUY",
			"size":"2",
			"status":"MATCHED",
			"timestamp":"1672290688",
			"type":"TRADE"
		}
	]`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(DecodeEvents()) = %d, want 2", len(events))
	}
	if events[0].Order == nil || events[0].Order.Type != "UPDATE" {
		t.Fatalf("unexpected order event: %+v", events[0])
	}
	if events[1].Fill == nil || events[1].Fill.Status != "MATCHED" {
		t.Fatalf("unexpected fill event: %+v", events[1])
	}
	if len(events[1].Fill.MakerOrders) != 1 || events[1].Fill.MakerOrders[0].OrderID != "ord-1" {
		t.Fatalf("unexpected maker orders: %+v", events[1].Fill.MakerOrders)
	}
}

func TestDecodeEventsIgnoresRawPONGHeartbeat(t *testing.T) {
	events, err := DecodeEvents([]byte("PONG"))
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("len(DecodeEvents()) = %d, want 0", len(events))
	}
}

func validCreds() polyauth.APICredentials {
	return polyauth.APICredentials{
		Key:        "key-1",
		Secret:     "secret-1",
		Passphrase: "pass-1",
	}
}
