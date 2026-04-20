package market

import (
	"context"
	"strconv"
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

func (c *Client) SubscribeMarket(ctx context.Context, assetIDs []int64) error {
	if len(assetIDs) == 0 {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.subscribe_market", Message: "assetIDs is required"}
	}

	payload := map[string]any{
		"type":      "subscribe",
		"channel":   "market",
		"assets_ids": assetIDs,
	}
	return c.ws.SubscribeJSON(ctx, "market:"+joinInt64(assetIDs), payload)
}

func joinInt64(ids []int64) string {
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(id, 10))
	}
	return b.String()
}

