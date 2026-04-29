package market

import (
	"bytes"
	"encoding/json"
	"strings"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

type Event struct {
	Book        *BookEvent
	PriceChange *PriceChangeEvent
	BestBidAsk  *BestBidAskEvent
}

type BookEvent struct {
	EventType string      `json:"event_type"`
	AssetID   string      `json:"asset_id"`
	Market    string      `json:"market"`
	Bids      []BookLevel `json:"bids"`
	Asks      []BookLevel `json:"asks"`
	Timestamp string      `json:"timestamp"`
	Hash      string      `json:"hash"`
}

type BookLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type PriceChangeEvent struct {
	Market       string        `json:"market"`
	PriceChanges []PriceChange `json:"price_changes"`
	Timestamp    string        `json:"timestamp"`
	EventType    string        `json:"event_type"`
}

type PriceChange struct {
	AssetID string `json:"asset_id"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Side    string `json:"side"`
	Hash    string `json:"hash"`
	BestBid string `json:"best_bid"`
	BestAsk string `json:"best_ask"`
}

type BestBidAskEvent struct {
	EventType string `json:"event_type"`
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	BestBid   string `json:"best_bid"`
	BestAsk   string `json:"best_ask"`
	Spread    string `json:"spread"`
	Timestamp string `json:"timestamp"`
}

func DecodeEvents(raw []byte) ([]Event, error) {
	var batch []json.RawMessage
	if err := json.Unmarshal(raw, &batch); err != nil {
		var single json.RawMessage
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			return nil, decodeError("market.decode", raw, err2)
		}
		batch = []json.RawMessage{single}
	}

	events := make([]Event, 0, len(batch))
	for _, item := range batch {
		var meta eventMeta
		if err := json.Unmarshal(item, &meta); err != nil {
			return nil, decodeError("market.decode", item, err)
		}

		switch meta.kind() {
		case "book":
			event, err := decodeBookEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_book", item, err)
			}
			events = append(events, Event{Book: &event})
		case "price_change":
			event, err := decodePriceChangeEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_price_change", item, err)
			}
			events = append(events, Event{PriceChange: &event})
		case "best_bid_ask":
			event, err := decodeBestBidAskEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_best_bid_ask", item, err)
			}
			events = append(events, Event{BestBidAsk: &event})
		}
	}
	return events, nil
}

type eventMeta struct {
	EventType string `json:"event_type"`
	Event     string `json:"event"`
}

func (m eventMeta) kind() string {
	if s := strings.TrimSpace(m.EventType); s != "" {
		return s
	}
	return strings.TrimSpace(m.Event)
}

func decodeBookEvent(raw []byte) (BookEvent, error) {
	var payload struct {
		eventMeta
		AssetID   string          `json:"asset_id"`
		Market    string          `json:"market"`
		Bids      []BookLevel     `json:"bids"`
		Asks      []BookLevel     `json:"asks"`
		Timestamp json.RawMessage `json:"timestamp"`
		Hash      string          `json:"hash"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return BookEvent{}, err
	}

	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return BookEvent{}, err
	}

	return BookEvent{
		EventType: payload.kind(),
		AssetID:   payload.AssetID,
		Market:    payload.Market,
		Bids:      payload.Bids,
		Asks:      payload.Asks,
		Timestamp: timestamp,
		Hash:      payload.Hash,
	}, nil
}

func decodePriceChangeEvent(raw []byte) (PriceChangeEvent, error) {
	var payload struct {
		eventMeta
		Market       string          `json:"market"`
		PriceChanges []PriceChange   `json:"price_changes"`
		Timestamp    json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return PriceChangeEvent{}, err
	}

	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return PriceChangeEvent{}, err
	}

	return PriceChangeEvent{
		EventType:    payload.kind(),
		Market:       payload.Market,
		PriceChanges: payload.PriceChanges,
		Timestamp:    timestamp,
	}, nil
}

func decodeBestBidAskEvent(raw []byte) (BestBidAskEvent, error) {
	var payload struct {
		eventMeta
		Market    string          `json:"market"`
		AssetID   string          `json:"asset_id"`
		BestBid   string          `json:"best_bid"`
		BestAsk   string          `json:"best_ask"`
		Spread    string          `json:"spread"`
		Timestamp json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return BestBidAskEvent{}, err
	}

	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return BestBidAskEvent{}, err
	}

	return BestBidAskEvent{
		EventType: payload.kind(),
		Market:    payload.Market,
		AssetID:   payload.AssetID,
		BestBid:   payload.BestBid,
		BestAsk:   payload.BestAsk,
		Spread:    payload.Spread,
		Timestamp: timestamp,
	}, nil
}

func decodeTimestamp(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}

	var s string
	strErr := json.Unmarshal(raw, &s)
	if strErr == nil {
		return s, nil
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var num json.Number
	if err := dec.Decode(&num); err == nil {
		return num.String(), nil
	}

	return "", strErr
}

func decodeError(op string, raw []byte, err error) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrDecode,
		Op:      op,
		Message: err.Error(),
		Cause:   err,
		RawBody: raw,
	}
}
