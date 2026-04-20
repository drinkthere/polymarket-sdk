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
	b, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c.TrackSubscription("sub:test", b)
	if err := c.Write(ctx, b); err != nil {
		t.Fatalf("Write: %v", err)
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

func TestClient_Read_DeliversApplicationMessages(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		if err := c.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
			t.Errorf("WriteMessage: %v", err)
			return
		}

		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(ClientConfig{URL: "ws" + srv.URL[len("http"):]})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	got, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected message: %q", string(got))
	}
}

func TestClient_Reconnect_DoesNotReplayUntrackedSubscriptions(t *testing.T) {
	t.Parallel()

	var conns atomic.Int64
	firstConnMsg := make(chan []byte, 1)
	secondConnSawMsg := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		n := conns.Add(1)

		if n == 1 {
			_, payload, err := c.ReadMessage()
			if err != nil {
				t.Errorf("ReadMessage: %v", err)
				return
			}
			firstConnMsg <- payload
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"), time.Now().Add(time.Second))
			return
		}

		_ = c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		_, payload, err := c.ReadMessage()
		if err == nil {
			secondConnSawMsg <- payload
		}
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
	b, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c.TrackSubscription("sub:test", b)
	if err := c.Write(ctx, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	c.UntrackSubscription("sub:test")

	select {
	case <-firstConnMsg:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for first subscribe: %v", ctx.Err())
	}

	select {
	case payload := <-secondConnSawMsg:
		t.Fatalf("unexpected replay on second conn: %s", string(payload))
	case <-ctx.Done():
		t.Fatalf("timeout waiting for reconnect: %v", ctx.Err())
	case <-time.After(750 * time.Millisecond):
	}

	if conns.Load() < 2 {
		t.Fatalf("expected at least 2 connections, got %d", conns.Load())
	}
}
