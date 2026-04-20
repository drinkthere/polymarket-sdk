package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

	connMu sync.RWMutex
	conn   *websocket.Conn

	sessionMu     sync.Mutex
	sessionCancel context.CancelFunc

	reconnectMu sync.Mutex
	reconnecting bool

	closedMu sync.Mutex
	closed   bool

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

	c := &Client{
		cfg: cfg,
		dialer: d,
		subs: make(map[string][]byte),
	}
	c.cfg.URL = url
	c.cfg.WriteTimeout = wt
	c.cfg.ReconnectBackoff = backoff
	return c, nil
}

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
		return conn.Close()
	}
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

	c.swapConn(conn)
	c.startSession(conn)

	if replay {
		_ = c.replaySubscriptions(context.Background())
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
	c.sessionMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	c.sessionCancel = cancel
	c.sessionMu.Unlock()

	if c.cfg.PingInterval > 0 {
		go c.keepaliveLoop(ctx, conn, c.cfg.PingInterval)
	}
	go c.readLoop(ctx, conn)
}

func (c *Client) keepaliveLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.cfg.WriteTimeout))
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, _, err := conn.ReadMessage(); err != nil {
			if c.cfg.Reconnect && !c.isClosed() {
				c.startReconnect()
			}
			return
		}
	}
}

func (c *Client) startReconnect() {
	c.reconnectMu.Lock()
	if c.reconnecting || c.isClosed() {
		c.reconnectMu.Unlock()
		return
	}
	c.reconnecting = true
	c.reconnectMu.Unlock()

	go func() {
		defer func() {
			c.reconnectMu.Lock()
			c.reconnecting = false
			c.reconnectMu.Unlock()
		}()

		for {
			if c.isClosed() {
				return
			}
			time.Sleep(c.cfg.ReconnectBackoff)
			if err := c.connect(context.Background(), true); err == nil {
				return
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
	return c.WriteText(ctx, b)
}

func (c *Client) WriteText(ctx context.Context, payload []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := c.connOrErr()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(c.cfg.WriteTimeout)
	_ = conn.SetWriteDeadline(deadline)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrNetwork, Op: "ws.write", URL: c.cfg.URL, Message: err.Error(), Cause: err}
	}
	return nil
}

func (c *Client) SubscribeJSON(ctx context.Context, key string, v any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "ws.subscribe_json", Message: err.Error(), Cause: err}
	}
	if strings.TrimSpace(key) != "" {
		c.subsMu.Lock()
		c.subs[key] = b
		c.subsMu.Unlock()
	}
	return c.WriteText(ctx, b)
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
		if err := c.WriteText(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

