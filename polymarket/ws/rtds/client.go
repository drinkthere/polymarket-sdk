package rtds

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/ws"
)

type Config struct {
	URL string

	PingInterval time.Duration

	Reconnect        bool
	ReconnectBackoff time.Duration
}

type Client struct {
	ws *ws.Client
}

type SubscribeRequest struct {
	Symbol string
}

type UnsubscribeRequest struct {
	Symbol string
}

func NewClient(cfg Config) (*Client, error) {
	wsc, err := ws.NewClient(ws.ClientConfig{
		URL:              cfg.URL,
		PingInterval:     cfg.PingInterval,
		Reconnect:        cfg.Reconnect,
		ReconnectBackoff: cfg.ReconnectBackoff,
	})
	if err != nil {
		return nil, err
	}
	return &Client{ws: wsc}, nil
}

func (c *Client) Connect(ctx context.Context) error { return c.ws.Connect(ctx) }
func (c *Client) Close() error                      { return c.ws.Close() }

func (c *Client) Read(ctx context.Context) ([]byte, error) { return c.ws.Read(ctx) }

func (c *Client) Subscribe(ctx context.Context, req SubscribeRequest) error {
	s := strings.TrimSpace(req.Symbol)
	if s == "" {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.subscribe", Message: "symbol is required"}
	}
	pair := strings.ToLower(s) + "usdt"
	filters := fmt.Sprintf(`{"symbol":"%s"}`, pair) // API expects this as a JSON string, not an object.

	payload := map[string]any{
		"type":    "subscribe",
		"channel": "crypto",
		"filters": filters,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.subscribe", Message: err.Error(), Cause: err}
	}

	if err := c.ws.Write(ctx, b); err != nil {
		return err
	}
	c.ws.TrackSubscription("crypto:"+pair, b)
	return nil
}

func (c *Client) Unsubscribe(ctx context.Context, req UnsubscribeRequest) error {
	s := strings.TrimSpace(req.Symbol)
	if s == "" {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.unsubscribe", Message: "symbol is required"}
	}
	pair := strings.ToLower(s) + "usdt"
	filters := fmt.Sprintf(`{"symbol":"%s"}`, pair) // API expects this as a JSON string, not an object.

	payload := map[string]any{
		"type":    "unsubscribe",
		"channel": "crypto",
		"filters": filters,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.unsubscribe", Message: err.Error(), Cause: err}
	}

	if err := c.ws.Write(ctx, b); err != nil {
		return err
	}
	c.ws.UntrackSubscription("crypto:" + pair)
	return nil
}

func (c *Client) SubscribeCrypto(ctx context.Context, symbol string) error {
	return c.Subscribe(ctx, SubscribeRequest{Symbol: symbol})
}
