package market

import (
	"context"
	"sort"
	"strings"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/ws"
)

// ChannelClient implements the "market channel" websocket protocol used by marketdata,
// where subscribe/unsubscribe are expressed as an "operation" and "assets_ids" are token IDs.
type ChannelClient struct {
	ws *ws.Client
}

type ChannelSubscribeRequest struct {
	AssetIDs []string
}

type ChannelUnsubscribeRequest struct {
	AssetIDs []string
}

func NewChannelClient(cfg Config) (*ChannelClient, error) {
	wsc, err := ws.NewClient(ws.ClientConfig{
		URL:              cfg.URL,
		Header:           cfg.Header,
		Dialer:           cfg.Dialer,
		WriteTimeout:     cfg.WriteTimeout,
		PingInterval:     cfg.PingInterval,
		Reconnect:        cfg.Reconnect,
		ReconnectBackoff: cfg.ReconnectBackoff,
	})
	if err != nil {
		return nil, err
	}
	return &ChannelClient{ws: wsc}, nil
}

func (c *ChannelClient) Connect(ctx context.Context) error { return c.ws.Connect(ctx) }
func (c *ChannelClient) Close() error                      { return c.ws.Close() }
func (c *ChannelClient) Errors() <-chan error              { return c.ws.Errors() }

func (c *ChannelClient) Read(ctx context.Context) ([]byte, error) { return c.ws.Read(ctx) }

func (c *ChannelClient) ReadMessage(ctx context.Context) (Message, error) {
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

func (c *ChannelClient) Subscribe(ctx context.Context, req ChannelSubscribeRequest) error {
	ids, key, err := normalizeAssetIDs(req.AssetIDs)
	if err != nil {
		return err
	}

	payload := channelSubscribePayload{
		AssetsIDs:            ids,
		Operation:            "subscribe",
		CustomFeatureEnabled: true,
	}
	return c.ws.SubscribeJSON(ctx, key, payload)
}

func (c *ChannelClient) Unsubscribe(ctx context.Context, req ChannelUnsubscribeRequest) error {
	ids, key, err := normalizeAssetIDs(req.AssetIDs)
	if err != nil {
		return err
	}

	payload := channelSubscribePayload{
		AssetsIDs: ids,
		Operation: "unsubscribe",
	}
	if err := c.ws.WriteJSON(ctx, payload); err != nil {
		return err
	}
	c.ws.UntrackSubscription(key)
	return nil
}

type channelSubscribePayload struct {
	AssetsIDs            []string `json:"assets_ids"`
	Operation            string   `json:"operation"`
	CustomFeatureEnabled bool     `json:"custom_feature_enabled,omitempty"`
}

func normalizeAssetIDs(assetIDs []string) (norm []string, key string, err error) {
	if len(assetIDs) == 0 {
		return nil, "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.channel.normalize", Message: "assetIDs is required"}
	}
	norm = make([]string, 0, len(assetIDs))
	for _, id := range assetIDs {
		s := strings.TrimSpace(id)
		if s == "" {
			return nil, "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "market.channel.normalize", Message: "assetID is required"}
		}
		norm = append(norm, s)
	}
	sort.Strings(norm)
	// Dedup after sort to keep subscription keys stable.
	out := norm[:0]
	var prev string
	for i, s := range norm {
		if i == 0 || s != prev {
			out = append(out, s)
			prev = s
		}
	}
	norm = out
	key = "market_ws:" + strings.Join(norm, ",")
	return norm, key, nil
}
