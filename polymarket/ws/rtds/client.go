package rtds

import (
	"context"
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
func (c *Client) Close() error                     { return c.ws.Close() }

func (c *Client) SubscribeCrypto(ctx context.Context, symbol string) error {
	s := strings.TrimSpace(symbol)
	if s == "" {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.subscribe_crypto", Message: "symbol is required"}
	}
	pair := strings.ToLower(s) + "usdt"
	filters := fmt.Sprintf(`{"symbol":"%s"}`, pair) // API expects this as a JSON string, not an object.

	payload := map[string]any{
		"type":    "subscribe",
		"channel": "crypto",
		"filters": filters,
	}
	return c.ws.SubscribeJSON(ctx, "crypto:"+pair, payload)
}

