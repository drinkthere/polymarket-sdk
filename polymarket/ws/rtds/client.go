package rtds

import (
	"context"
	"fmt"
	"net/http"
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

	WriteTimeout time.Duration
	PingInterval time.Duration

	Reconnect        bool
	ReconnectBackoff time.Duration
}

type Client struct {
	ws *ws.Client
}

type Channel string

const (
	ChannelCrypto Channel = "crypto"
	ChannelEquity Channel = "equity"
)

type SubscribeRequest struct {
	// Channel defaults to ChannelCrypto for backward compatibility.
	Channel Channel
	Symbol  string
}

type UnsubscribeRequest struct {
	// Channel defaults to ChannelCrypto for backward compatibility.
	Channel Channel
	Symbol  string
}

type SubscribeItemRequest struct {
	// Item is a Polymarket RTDS subscription selector in the form:
	//   crypto:<symbol> / crypto_prices:<pair>
	//   equity:<symbol> / equity_prices:<symbol>
	//
	// Examples: "crypto:BTC", "crypto_prices:btcusdt", "equity:SPY".
	Item string
}

type UnsubscribeItemRequest struct {
	Item string
}

type Message struct {
	MessageType string
	Raw         []byte
}

func NewClient(cfg Config) (*Client, error) {
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
	return &Client{ws: wsc}, nil
}

func (c *Client) Connect(ctx context.Context) error { return c.ws.Connect(ctx) }
func (c *Client) Close() error                      { return c.ws.Close() }

func (c *Client) Read(ctx context.Context) ([]byte, error) { return c.ws.Read(ctx) }

func (c *Client) Subscribe(ctx context.Context, req SubscribeRequest) error {
	ch := req.Channel
	if strings.TrimSpace(string(ch)) == "" {
		ch = ChannelCrypto
	}
	s := strings.TrimSpace(req.Symbol)
	if s == "" {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.subscribe", Message: "symbol is required"}
	}
	keySymbol, err := normalizeKeySymbol(ch, s)
	if err != nil {
		return err
	}
	filters := fmt.Sprintf(`{"symbol":"%s"}`, keySymbol) // API expects this as a JSON string, not an object.

	payload := subscribePayload{
		Type:    "subscribe",
		Channel: string(ch),
		Filters: filters,
	}

	key := subscriptionKey(ch, keySymbol)
	if err := c.ws.SubscribeJSON(ctx, key, payload); err != nil {
		return err
	}
	return nil
}

func (c *Client) Unsubscribe(ctx context.Context, req UnsubscribeRequest) error {
	ch := req.Channel
	if strings.TrimSpace(string(ch)) == "" {
		ch = ChannelCrypto
	}
	s := strings.TrimSpace(req.Symbol)
	if s == "" {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.unsubscribe", Message: "symbol is required"}
	}
	keySymbol, err := normalizeKeySymbol(ch, s)
	if err != nil {
		return err
	}
	filters := fmt.Sprintf(`{"symbol":"%s"}`, keySymbol) // API expects this as a JSON string, not an object.

	payload := subscribePayload{
		Type:    "unsubscribe",
		Channel: string(ch),
		Filters: filters,
	}

	key := subscriptionKey(ch, keySymbol)
	if err := c.ws.WriteJSON(ctx, payload); err != nil {
		return err
	}
	c.ws.UntrackSubscription(key)
	return nil
}

func (c *Client) SubscribeCrypto(ctx context.Context, symbol string) error {
	return c.Subscribe(ctx, SubscribeRequest{Symbol: symbol})
}

func (c *Client) SubscribeEquity(ctx context.Context, symbol string) error {
	return c.Subscribe(ctx, SubscribeRequest{Channel: ChannelEquity, Symbol: symbol})
}

func (c *Client) SubscribeItem(ctx context.Context, req SubscribeItemRequest) error {
	ch, sym, err := parseItem(req.Item)
	if err != nil {
		return err
	}
	return c.Subscribe(ctx, SubscribeRequest{Channel: ch, Symbol: sym})
}

func (c *Client) UnsubscribeItem(ctx context.Context, req UnsubscribeItemRequest) error {
	ch, sym, err := parseItem(req.Item)
	if err != nil {
		return err
	}
	return c.Unsubscribe(ctx, UnsubscribeRequest{Channel: ch, Symbol: sym})
}

func (c *Client) ReadMessage(ctx context.Context) (Message, error) {
	payload, err := c.ws.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	cp := append([]byte(nil), payload...)
	return Message{
		MessageType: ws.InferMessageType(cp),
		Raw:         cp,
	}, nil
}

type subscribePayload struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Filters string `json:"filters"`
}

func subscriptionKey(ch Channel, keySymbol string) string {
	// Key is internal to the SDK to support reconnect subscription replay.
	return "rtds:" + strings.ToLower(string(ch)) + ":" + keySymbol
}

func normalizeKeySymbol(ch Channel, symbol string) (string, error) {
	switch ch {
	case ChannelCrypto:
		s := strings.ToLower(strings.TrimSpace(symbol))
		if s == "" {
			return "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.normalize", Message: "symbol is required"}
		}
		if strings.HasSuffix(s, "usdt") {
			s = strings.TrimSuffix(s, "usdt")
		}
		return s + "usdt", nil
	case ChannelEquity:
		s := strings.TrimSpace(symbol)
		if s == "" {
			return "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.normalize", Message: "symbol is required"}
		}
		return strings.ToUpper(s), nil
	default:
		return "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.normalize", Message: "unsupported channel: " + string(ch)}
	}
}

func parseItem(item string) (Channel, string, error) {
	parts := strings.Split(strings.TrimSpace(item), ":")
	if len(parts) != 2 {
		return "", "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.parse_item", Message: "expected asset_class:inst_id"}
	}
	left := strings.ToLower(strings.TrimSpace(parts[0]))
	right := strings.TrimSpace(parts[1])
	if left == "" {
		return "", "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.parse_item", Message: "asset_class is required"}
	}
	if right == "" {
		return "", "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.parse_item", Message: "inst_id is required"}
	}

	switch left {
	case "crypto", "crypto_prices":
		return ChannelCrypto, right, nil
	case "equity", "equity_prices":
		return ChannelEquity, right, nil
	default:
		return "", "", &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "rtds.parse_item", Message: "unsupported asset_class: " + left}
	}
}
