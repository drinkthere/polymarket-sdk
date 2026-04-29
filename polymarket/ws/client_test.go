package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
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

func TestClient_AppPingInterval_SendsTextPING(t *testing.T) {
	t.Parallel()

	textCh := make(chan []byte, 1)

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
			if mt == websocket.TextMessage {
				textCh <- append([]byte(nil), payload...)
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(ClientConfig{
		URL:             "ws" + srv.URL[len("http"):],
		AppPingInterval: 25 * time.Millisecond,
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
	case got := <-textCh:
		if string(got) != "PING" {
			t.Fatalf("expected text PING, got %q", string(got))
		}
	case <-ctx.Done():
		t.Fatalf("expected text PING before timeout: %v", ctx.Err())
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

func TestClient_Read_DeliversPostReconnectMessages(t *testing.T) {
	t.Parallel()

	var conns atomic.Int64
	firstConnSub := make(chan []byte, 1)
	secondConnSub := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		switch conns.Add(1) {
		case 1:
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("ReadMessage(first subscribe): %v", err)
				return
			}
			firstConnSub <- payload
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"), time.Now().Add(time.Second))
		case 2:
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("ReadMessage(replayed subscribe): %v", err)
				return
			}
			secondConnSub <- payload
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"update","symbol":"btcusdt","timestamp":2}`)); err != nil {
				t.Errorf("WriteMessage(post-reconnect): %v", err)
				return
			}

			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		default:
			t.Errorf("unexpected extra connection: %d", conns.Load())
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

	sub := map[string]any{"type": "subscribe", "channel": "test", "symbol": "btcusdt"}
	b, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("marshal subscribe: %v", err)
	}

	c.TrackSubscription("sub:test:btcusdt", b)
	if err := c.Write(ctx, b); err != nil {
		t.Fatalf("Write subscribe: %v", err)
	}

	select {
	case <-firstConnSub:
	case <-ctx.Done():
		t.Fatalf("timeout waiting first subscribe: %v", ctx.Err())
	}
	select {
	case <-secondConnSub:
	case <-ctx.Done():
		t.Fatalf("timeout waiting replayed subscribe: %v", ctx.Err())
	}

	got, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("Read post-reconnect: %v", err)
	}
	if string(got) != `{"type":"update","symbol":"btcusdt","timestamp":2}` {
		t.Fatalf("unexpected post-reconnect payload: %s", string(got))
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

func TestClient_Connect_WhileConnected_ReconnectTrue_DoesNotReconnectThrash(t *testing.T) {
	t.Parallel()

	var conns atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		conns.Add(1)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 1: %v", err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect 2: %v", err)
	}

	// If the old session triggers reconnect on intentional swap, we'd see more than 2 dials quickly.
	time.Sleep(200 * time.Millisecond)
	if got := conns.Load(); got != 2 {
		t.Fatalf("expected exactly 2 connections, got %d", got)
	}
}

func TestClient_CloseConcurrentWithConnect_DoesNotLeaveActiveConnOrSession(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	unblockDial := make(chan struct{})
	d := &websocket.Dialer{
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case <-unblockDial:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			var nd net.Dialer
			return nd.DialContext(ctx, network, addr)
		},
	}

	c, err := NewClient(ClientConfig{
		URL:    "ws" + srv.URL[len("http"):],
		Dialer: d,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	connectDone := make(chan error, 1)
	go func() { connectDone <- c.Connect(ctx) }()

	// Give Connect() enough time to enter the dial path.
	time.Sleep(25 * time.Millisecond)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(unblockDial)

	err = <-connectDone
	if err == nil {
		t.Fatalf("expected Connect to fail after Close")
	}
	var pe *polyerrors.Error
	if !errors.As(err, &pe) || pe.Kind != polyerrors.ErrClosed {
		t.Fatalf("expected ErrClosed, got %v", err)
	}

	c.connMu.RLock()
	if c.conn != nil {
		c.connMu.RUnlock()
		t.Fatalf("expected no active conn after Close+Connect race")
	}
	c.connMu.RUnlock()

	c.sessionMu.Lock()
	if c.sessionCancel != nil {
		c.sessionMu.Unlock()
		t.Fatalf("expected no active session after Close+Connect race")
	}
	c.sessionMu.Unlock()
}

func TestClient_ConcurrentPingAndWrites(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()

		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(ClientConfig{
		URL:          "ws" + srv.URL[len("http"):],
		PingInterval: 1 * time.Millisecond,
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

	const workers = 8
	const perWorker = 50

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			for n := 0; n < perWorker; n++ {
				if err := c.Write(ctx, []byte("msg")); err != nil {
					errCh <- errors.New("write worker " + string(rune('A'+i)) + ": " + err.Error())
					return
				}
			}
		}()
	}
	wg.Wait()

	select {
	case err := <-errCh:
		t.Fatalf("concurrent writes failed: %v", err)
	default:
	}
}

func TestClient_Write_ContextDeadlineExceeded_IsTimeoutKind(t *testing.T) {
	t.Parallel()

	c, err := NewClient(ClientConfig{URL: "ws://localhost"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err = c.Write(ctx, []byte("x"))
	if err == nil {
		t.Fatalf("expected error")
	}
	var pe *polyerrors.Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *polyerrors.Error, got %T %v", err, err)
	}
	if pe.Kind != polyerrors.ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %q (%v)", pe.Kind, pe)
	}
}
