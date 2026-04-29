package market

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/ws"
	"github.com/gorilla/websocket"
)

type Config struct {
	URL string

	Header *http.Header
	Dialer *websocket.Dialer

	WriteTimeout    time.Duration
	PingInterval    time.Duration
	AppPingInterval time.Duration
	AppPingPayload  []byte

	Reconnect        bool
	ReconnectBackoff time.Duration
}

type Client struct {
	ws *ws.Client
}

type Message struct {
	MessageType string
	Raw         []byte
}

type SubscribeRequest struct {
	AssetIDs []int64
}

type UnsubscribeRequest struct {
	AssetIDs []int64
}

func NewClient(cfg Config) (*Client, error) {
	wsc, err := ws.NewClient(ws.ClientConfig{
		URL:              cfg.URL,
		Header:           cfg.Header,
		Dialer:           cfg.Dialer,
		WriteTimeout:     cfg.WriteTimeout,
		PingInterval:     cfg.PingInterval,
		AppPingInterval:  cfg.AppPingInterval,
		AppPingPayload:   cfg.AppPingPayload,
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

func (c *Client) ReadMessage(ctx context.Context) (Message, error) {
	payload, err := c.ws.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	cp := append([]byte(nil), payload...)
	messageType, err := inferMessageType(cp)
	if err != nil {
		return Message{}, err
	}
	return Message{
		MessageType: messageType,
		Raw:         cp,
	}, nil
}

func (c *Client) Subscribe(ctx context.Context, req SubscribeRequest) error {
	if len(req.AssetIDs) == 0 {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.subscribe", Message: "assetIDs is required"}
	}

	payload := map[string]any{
		"type":       "subscribe",
		"channel":    "market",
		"assets_ids": req.AssetIDs,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.subscribe", Message: err.Error(), Cause: err}
	}
	key := "market:" + joinInt64(req.AssetIDs)
	if err := c.ws.Write(ctx, b); err != nil {
		return err
	}
	c.ws.TrackSubscription(key, b)
	return nil
}

func (c *Client) Unsubscribe(ctx context.Context, req UnsubscribeRequest) error {
	if len(req.AssetIDs) == 0 {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.unsubscribe", Message: "assetIDs is required"}
	}

	payload := map[string]any{
		"type":       "unsubscribe",
		"channel":    "market",
		"assets_ids": req.AssetIDs,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.unsubscribe", Message: err.Error(), Cause: err}
	}
	key := "market:" + joinInt64(req.AssetIDs)
	if err := c.ws.Write(ctx, b); err != nil {
		return err
	}
	c.ws.UntrackSubscription(key)
	return nil
}

func (c *Client) SubscribeMarket(ctx context.Context, assetIDs []int64) error {
	return c.Subscribe(ctx, SubscribeRequest{AssetIDs: assetIDs})
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
