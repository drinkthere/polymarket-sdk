package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClient_Keepalive_SendsPing(t *testing.T) {
	t.Parallel()

	pingCh := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		c.SetPingHandler(func(string) error {
			select {
			case pingCh <- struct{}{}:
			default:
			}
			return nil
		})

		// Control frames are processed during reads.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(ClientConfig{
		URL:          "ws" + srv.URL[len("http"):],
		PingInterval: 25 * time.Millisecond,
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

	select {
	case <-pingCh:
	case <-ctx.Done():
		t.Fatalf("expected ping before timeout: %v", ctx.Err())
	}
}

func TestClient_Reconnect_ReplaysSubscriptions(t *testing.T) {
	t.Parallel()

	var conns atomic.Int64
	firstConnMsg := make(chan []byte, 1)
	secondConnMsg := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		n := conns.Add(1)

		_, payload, err := c.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage: %v", err)
			return
		}

		if n == 1 {
			firstConnMsg <- payload
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"), time.Now().Add(time.Second))
			return
		}

		secondConnMsg <- payload
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(ClientConfig{
		URL:              "ws" + srv.URL[len("http"):],
		Reconnect:        true,
		ReconnectBackoff: 10 * time.Millisecond,
		PingInterval:     0,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	sub := map[string]any{"type": "subscribe", "channel": "test", "n": 1}
	if err := c.SubscribeJSON(ctx, "sub:test", sub); err != nil {
		t.Fatalf("SubscribeJSON: %v", err)
	}

	var msg1, msg2 []byte
	select {
	case msg1 = <-firstConnMsg:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for first subscribe: %v", ctx.Err())
	}
	select {
	case msg2 = <-secondConnMsg:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for replayed subscribe: %v", ctx.Err())
	}

	var got1, got2 map[string]any
	if err := json.Unmarshal(msg1, &got1); err != nil {
		t.Fatalf("unmarshal msg1: %v", err)
	}
	if err := json.Unmarshal(msg2, &got2); err != nil {
		t.Fatalf("unmarshal msg2: %v", err)
	}
	if got1["type"] != got2["type"] || got1["channel"] != got2["channel"] || got1["n"] != got2["n"] {
		t.Fatalf("replayed subscription mismatch: msg1=%v msg2=%v", got1, got2)
	}
	if conns.Load() < 2 {
		t.Fatalf("expected at least 2 connections, got %d", conns.Load())
	}
}

