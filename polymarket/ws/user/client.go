package user

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"
	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/ws"
	"github.com/gorilla/websocket"
)

type Config struct {
	URL string

	Header *http.Header
	Dialer *websocket.Dialer

	WriteTimeout     time.Duration
	PingInterval     time.Duration
	AppPingInterval  time.Duration
	AppPingPayload   []byte
	Reconnect        bool
	ReconnectBackoff time.Duration
}

type SubscribeRequest struct {
	Credentials polyauth.APICredentials
	Markets     []string
	InitialDump bool
}

type Message struct {
	Events []Event
	Raw    []byte
}

type Event struct {
	Order *OrderEvent
	Fill  *FillEvent
}

type OrderEvent struct {
	ID              string   `json:"id"`
	AssetID         string   `json:"asset_id"`
	AssociateTrades []string `json:"associate_trades"`
	Market          string   `json:"market"`
	OrderOwner      string   `json:"order_owner"`
	OriginalSize    string   `json:"original_size"`
	Outcome         string   `json:"outcome"`
	Owner           string   `json:"owner"`
	Price           string   `json:"price"`
	Side            string   `json:"side"`
	SizeMatched     string   `json:"size_matched"`
	Status          string   `json:"status"`
	Timestamp       string   `json:"timestamp"`
	Type            string   `json:"type"`
}

type FillMakerOrder struct {
	AssetID       string `json:"asset_id"`
	MatchedAmount string `json:"matched_amount"`
	OrderID       string `json:"order_id"`
	Outcome       string `json:"outcome"`
	Owner         string `json:"owner"`
	Price         string `json:"price"`
	Side          string `json:"side"`
}

type FillEvent struct {
	ID          string           `json:"id"`
	AssetID     string           `json:"asset_id"`
	LastUpdate  string           `json:"last_update"`
	MakerOrders []FillMakerOrder `json:"maker_orders"`
	Market      string           `json:"market"`
	Owner       string           `json:"owner"`
	Price       string           `json:"price"`
	Side        string           `json:"side"`
	Size        string           `json:"size"`
	Status      string           `json:"status"`
	Timestamp   string           `json:"timestamp"`
	Type        string           `json:"type"`
}

type Client struct {
	ws *ws.Client
}

func NewClient(cfg Config) (*Client, error) {
	wsc, err := ws.NewClient(polymarketWSConfig(cfg))
	if err != nil {
		return nil, err
	}
	return &Client{ws: wsc}, nil
}

func polymarketWSConfig(cfg Config) ws.ClientConfig {
	pingInterval := cfg.PingInterval
	appPingInterval := cfg.AppPingInterval
	if appPingInterval <= 0 && pingInterval > 0 {
		appPingInterval = pingInterval
		pingInterval = 0
	}
	return ws.ClientConfig{
		URL:              cfg.URL,
		Header:           cfg.Header,
		Dialer:           cfg.Dialer,
		WriteTimeout:     cfg.WriteTimeout,
		PingInterval:     pingInterval,
		AppPingInterval:  appPingInterval,
		AppPingPayload:   cfg.AppPingPayload,
		Reconnect:        cfg.Reconnect,
		ReconnectBackoff: cfg.ReconnectBackoff,
	}
}

func (c *Client) Connect(ctx context.Context) error { return c.ws.Connect(ctx) }
func (c *Client) Close() error                      { return c.ws.Close() }

func (c *Client) Subscribe(ctx context.Context, req SubscribeRequest) error {
	if !req.Credentials.Valid() {
		return &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "user.subscribe", Message: "api credentials are required"}
	}

	initialDump := req.InitialDump
	if !req.InitialDump {
		initialDump = true
	}

	payload := map[string]any{
		"auth": map[string]string{
			"apiKey":     req.Credentials.Key,
			"secret":     req.Credentials.Secret,
			"passphrase": req.Credentials.Passphrase,
		},
		"markets":      req.Markets,
		"type":         "user",
		"initial_dump": initialDump,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "user.subscribe", Message: err.Error(), Cause: err}
	}
	if err := c.ws.Write(ctx, raw); err != nil {
		return err
	}

	key := "user:" + strings.Join(req.Markets, ",")
	c.ws.TrackSubscription(key, raw)
	return nil
}

func (c *Client) ReadMessage(ctx context.Context) (Message, error) {
	raw, err := c.ws.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	events, err := DecodeEvents(raw)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Events: events,
		Raw:    append([]byte(nil), raw...),
	}, nil
}

func DecodeEvents(raw []byte) ([]Event, error) {
	if isRawPONG(raw) {
		return nil, nil
	}

	var batch []json.RawMessage
	if err := json.Unmarshal(raw, &batch); err != nil {
		var single json.RawMessage
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			return nil, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "user.decode", Message: err2.Error(), Cause: err2, RawBody: raw}
		}
		batch = []json.RawMessage{single}
	}

	events := make([]Event, 0, len(batch))
	for _, item := range batch {
		var meta struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal(item, &meta); err != nil {
			return nil, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "user.decode", Message: err.Error(), Cause: err, RawBody: item}
		}

		switch meta.EventType {
		case "order":
			var order OrderEvent
			if err := json.Unmarshal(item, &order); err != nil {
				return nil, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "user.decode_order", Message: err.Error(), Cause: err, RawBody: item}
			}
			events = append(events, Event{Order: &order})
		case "trade":
			var fill FillEvent
			if err := json.Unmarshal(item, &fill); err != nil {
				return nil, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "user.decode_trade", Message: err.Error(), Cause: err, RawBody: item}
			}
			events = append(events, Event{Fill: &fill})
		}
	}
	return events, nil
}

func isRawPONG(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("PONG"))
}
