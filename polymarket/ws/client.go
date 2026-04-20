package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/gorilla/websocket"
)

type ClientConfig struct {
	URL string

	Header *http.Header
	Dialer *websocket.Dialer

	WriteTimeout time.Duration
	PingInterval time.Duration

	Reconnect        bool
	ReconnectBackoff time.Duration
}

type Client struct {
	cfg ClientConfig

	dialer *websocket.Dialer

	lifecycleMu sync.Mutex

	closeCtx    context.Context
	closeCancel context.CancelFunc

	connMu sync.RWMutex
	conn   *websocket.Conn

	writeMu sync.Mutex

	sessionMu     sync.Mutex
	sessionCancel context.CancelFunc

	reconnectMu  sync.Mutex
	reconnecting bool

	closedMu sync.Mutex
	closed   bool

	readCh chan []byte
	errCh  chan error
	doneCh chan struct{}

	wg sync.WaitGroup

	subsMu sync.Mutex
	subs   map[string][]byte
}

const (
	defaultWriteTimeout     = 5 * time.Second
	defaultReconnectBackoff = 250 * time.Millisecond
)

func NewClient(cfg ClientConfig) (*Client, error) {
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "ws.new", Message: "url is required"}
	}
	if !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "ws.new", Message: "url must start with ws:// or wss://"}
	}

	d := cfg.Dialer
	if d == nil {
		d = websocket.DefaultDialer
	}

	wt := cfg.WriteTimeout
	if wt <= 0 {
		wt = defaultWriteTimeout
	}

	backoff := cfg.ReconnectBackoff
	if backoff <= 0 {
		backoff = defaultReconnectBackoff
	}

	closeCtx, closeCancel := context.WithCancel(context.Background())

	c := &Client{
		cfg:         cfg,
		dialer:      d,
		closeCtx:    closeCtx,
		closeCancel: closeCancel,
		readCh:      make(chan []byte, 256),
		errCh:       make(chan error, 16),
		doneCh:      make(chan struct{}),
		subs:        make(map[string][]byte),
	}
	c.cfg.URL = url
	c.cfg.WriteTimeout = wt
	c.cfg.ReconnectBackoff = backoff
	return c, nil
}

// Errors returns a channel where connection/session errors are published.
// The channel is never closed; consumers should stop when Close() is called or
// their own context ends.
func (c *Client) Errors() <-chan error { return c.errCh }

func (c *Client) isClosed() bool {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()
	return c.closed
}

func (c *Client) Close() error {
	c.closedMu.Lock()
	if c.closed {
		c.closedMu.Unlock()
		return nil
	}
	c.closed = true
	c.closedMu.Unlock()

	c.closeCancel()
	close(c.doneCh)

	c.sessionMu.Lock()
	cancel := c.sessionCancel
	c.sessionCancel = nil
	c.sessionMu.Unlock()
	if cancel != nil {
		cancel()
	}

	c.connMu.Lock()
	conn := c.conn
	c.conn = nil
	c.connMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}

	// Barrier: wait for any in-flight lifecycle transitions (Connect/startSession/startReconnect) to finish
	// their WaitGroup Add() calls before we begin waiting.
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()

	// Wait for background goroutines (read loop, keepalive loop, reconnect loop) to exit.
	c.wg.Wait()
	return nil
}

func (c *Client) Connect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return c.connect(ctx, false)
}

func (c *Client) connect(ctx context.Context, replay bool) error {
	if c.isClosed() {
		return &polyerrors.Error{Kind: polyerrors.ErrClosed, Op: "ws.connect", Message: "client is closed"}
	}

	hdr := http.Header{}
	if c.cfg.Header != nil {
		hdr = c.cfg.Header.Clone()
	}

	conn, _, err := c.dialer.DialContext(ctx, c.cfg.URL, hdr)
	if err != nil {
		kind := polyerrors.ErrNetwork
		if errors.Is(err, context.Canceled) {
			kind = polyerrors.ErrClosed
		} else if errors.Is(err, context.DeadlineExceeded) {
			kind = polyerrors.ErrTimeout
		}
		return &polyerrors.Error{Kind: kind, Op: "ws.connect", URL: c.cfg.URL, Message: err.Error(), Cause: err}
	}

	// Connect can race with Close(): never install a live connection/session after an intentional Close().
	if c.isClosed() {
		_ = conn.Close()
		return &polyerrors.Error{Kind: polyerrors.ErrClosed, Op: "ws.connect", Message: "client is closed"}
	}

	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	// Close could have acquired lifecycleMu first; re-check after we serialize lifecycle transitions.
	if c.isClosed() {
		_ = conn.Close()
		return &polyerrors.Error{Kind: polyerrors.ErrClosed, Op: "ws.connect", Message: "client is closed"}
	}

	c.swapConn(conn)
	c.startSession(conn)

	if replay {
		if err := c.replaySubscriptions(context.Background()); err != nil {
			// Drop the half-open session/conn and surface replay failure deterministically.
			c.swapConn(nil)
			return err
		}
	}
	return nil
}

func (c *Client) swapConn(conn *websocket.Conn) {
	c.sessionMu.Lock()
	cancel := c.sessionCancel
	c.sessionCancel = nil
	c.sessionMu.Unlock()
	if cancel != nil {
		cancel()
	}

	c.connMu.Lock()
	old := c.conn
	c.conn = conn
	c.connMu.Unlock()

	if old != nil {
		_ = old.Close()
	}
}

func (c *Client) startSession(conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	if c.isClosed() || conn == nil {
		cancel()
		return
	}

	c.sessionMu.Lock()
	c.sessionCancel = cancel
	c.sessionMu.Unlock()

	if c.cfg.PingInterval > 0 {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.keepaliveLoop(ctx, conn, c.cfg.PingInterval)
		}()
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.readLoop(ctx, conn)
	}()
}

func (c *Client) isActiveConn(conn *websocket.Conn) bool {
	c.connMu.RLock()
	cur := c.conn
	c.connMu.RUnlock()
	return cur != nil && cur == conn
}

func (c *Client) keepaliveLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !c.isActiveConn(conn) {
				return
			}
			c.writeMu.Lock()
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.cfg.WriteTimeout))
			c.writeMu.Unlock()
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.doneCh:
			return
		default:
		}

		mt, payload, err := conn.ReadMessage()
		if err != nil {
			// Ignore errors from stale connections (e.g. intentional swaps); only the active conn can drive lifecycle.
			if !c.isClosed() && c.isActiveConn(conn) && !errors.Is(err, context.Canceled) {
				c.emitErr(&polyerrors.Error{Kind: polyerrors.ErrNetwork, Op: "ws.read", URL: c.cfg.URL, Message: err.Error(), Cause: err})
			}
			if c.cfg.Reconnect && !c.isClosed() && c.isActiveConn(conn) {
				c.startReconnect()
			}
			return
		}

		if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
			continue
		}
		select {
		case c.readCh <- payload:
		case <-ctx.Done():
			return
		case <-c.doneCh:
			return
		}
	}
}

func (c *Client) startReconnect() {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.reconnectMu.Lock()
	if c.reconnecting || c.isClosed() {
		c.reconnectMu.Unlock()
		return
	}
	c.reconnecting = true
	c.reconnectMu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			c.reconnectMu.Lock()
			c.reconnecting = false
			c.reconnectMu.Unlock()
		}()

		for {
			if c.isClosed() {
				return
			}
			select {
			case <-c.doneCh:
				return
			case <-time.After(c.cfg.ReconnectBackoff):
			}

			if err := c.connect(c.closeCtx, true); err == nil {
				return
			} else {
				c.emitErr(err)
			}
		}
	}()
}

func (c *Client) connOrErr() (*websocket.Conn, error) {
	if c.isClosed() {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrClosed, Op: "ws.conn", Message: "client is closed"}
	}
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()
	if conn == nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrClosed, Op: "ws.conn", Message: "not connected"}
	}
	return conn, nil
}

func (c *Client) WriteJSON(ctx context.Context, v any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "ws.write_json", Message: err.Error(), Cause: err}
	}
	return c.Write(ctx, b)
}

func (c *Client) Write(ctx context.Context, payload []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		kind := polyerrors.ErrClosed
		if errors.Is(err, context.DeadlineExceeded) {
			kind = polyerrors.ErrTimeout
		}
		return &polyerrors.Error{Kind: kind, Op: "ws.write", Message: err.Error(), Cause: err}
	}

	conn, err := c.connOrErr()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(c.cfg.WriteTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}

	c.writeMu.Lock()
	_ = conn.SetWriteDeadline(deadline)
	werr := conn.WriteMessage(websocket.TextMessage, payload)
	c.writeMu.Unlock()
	if werr != nil {
		kind := polyerrors.ErrNetwork
		if errors.Is(werr, context.Canceled) {
			kind = polyerrors.ErrClosed
		} else if errors.Is(werr, context.DeadlineExceeded) || errors.Is(werr, os.ErrDeadlineExceeded) {
			kind = polyerrors.ErrTimeout
		} else if ne, ok := werr.(net.Error); ok && ne.Timeout() {
			kind = polyerrors.ErrTimeout
		}
		perr := &polyerrors.Error{Kind: kind, Op: "ws.write", URL: c.cfg.URL, Message: werr.Error(), Cause: werr}
		if c.isActiveConn(conn) {
			c.emitErr(perr)
		}
		return perr
	}
	return nil
}

func (c *Client) WriteText(ctx context.Context, payload []byte) error {
	return c.Write(ctx, payload)
}

func (c *Client) Read(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case b := <-c.readCh:
		return b, nil
	case <-c.doneCh:
		return nil, &polyerrors.Error{Kind: polyerrors.ErrClosed, Op: "ws.read", Message: "client is closed"}
	case <-ctx.Done():
		kind := polyerrors.ErrClosed
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = polyerrors.ErrTimeout
		}
		return nil, &polyerrors.Error{Kind: kind, Op: "ws.read", Message: ctx.Err().Error(), Cause: ctx.Err()}
	}
}

func (c *Client) TrackSubscription(key string, payload []byte) {
	k := strings.TrimSpace(key)
	if k == "" {
		return
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	c.subsMu.Lock()
	c.subs[k] = cp
	c.subsMu.Unlock()
}

func (c *Client) UntrackSubscription(key string) {
	k := strings.TrimSpace(key)
	if k == "" {
		return
	}
	c.subsMu.Lock()
	delete(c.subs, k)
	c.subsMu.Unlock()
}

func (c *Client) SubscribeJSON(ctx context.Context, key string, v any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "ws.subscribe_json", Message: err.Error(), Cause: err}
	}
	if err := c.Write(ctx, b); err != nil {
		return err
	}
	c.TrackSubscription(key, b)
	return nil
}

func (c *Client) replaySubscriptions(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.subsMu.Lock()
	snap := make([][]byte, 0, len(c.subs))
	for _, b := range c.subs {
		cp := make([]byte, len(b))
		copy(cp, b)
		snap = append(snap, cp)
	}
	c.subsMu.Unlock()

	for _, b := range snap {
		if err := c.Write(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) emitErr(err error) {
	if err == nil {
		return
	}
	select {
	case c.errCh <- err:
	default:
	}
}
