package market

import (
	"bytes"
	"encoding/json"
	"fmt"
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

		if meta.isFallback() {
			shouldDecode, err := classifyLegacyMarketEvent(meta.kind(), item)
			if err != nil {
				return nil, decodeError(decodeEventOp(meta.kind()), item, err)
			}
			if !shouldDecode {
				continue
			}
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

func (m eventMeta) isFallback() bool {
	return strings.TrimSpace(m.EventType) == "" && strings.TrimSpace(m.Event) != ""
}

func classifyLegacyMarketEvent(kind string, raw []byte) (bool, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, err
	}

	switch kind {
	case "book":
		assetID, hasAssetID := payload["asset_id"]
		bids, hasBids := payload["bids"]
		asks, hasAsks := payload["asks"]
		if !hasAssetID && !hasBids && !hasAsks {
			return false, nil
		}
		if err := requireNonEmptyJSONString(assetID, hasAssetID, "asset_id"); err != nil {
			return false, err
		}
		if !hasBids && !hasAsks {
			return false, fmt.Errorf("book payload requires bids or asks")
		}
		if err := requireJSONArray(bids, hasBids, "bids"); err != nil {
			return false, err
		}
		if err := requireJSONArray(asks, hasAsks, "asks"); err != nil {
			return false, err
		}
		return true, nil
	case "best_bid_ask":
		assetID, hasAssetID := payload["asset_id"]
		bestBid, hasBestBid := payload["best_bid"]
		bestAsk, hasBestAsk := payload["best_ask"]
		if !hasAssetID && !hasBestBid && !hasBestAsk {
			return false, nil
		}
		if err := requireNonEmptyJSONString(assetID, hasAssetID, "asset_id"); err != nil {
			return false, err
		}
		if err := requireJSONString(bestBid, hasBestBid, "best_bid"); err != nil {
			return false, err
		}
		if err := requireJSONString(bestAsk, hasBestAsk, "best_ask"); err != nil {
			return false, err
		}
		return true, nil
	case "price_change":
		priceChanges, hasPriceChanges := payload["price_changes"]
		if !hasPriceChanges {
			return false, nil
		}
		if err := requireJSONArray(priceChanges, hasPriceChanges, "price_changes"); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func requireNonEmptyJSONString(raw json.RawMessage, ok bool, field string) error {
	if err := requireJSONString(raw, ok, field); err != nil {
		return err
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

func requireJSONString(raw json.RawMessage, ok bool, field string) error {
	if !ok {
		return fmt.Errorf("%s is required", field)
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s must be a string: %w", field, err)
	}
	_ = value
	return nil
}

func requireJSONArray(raw json.RawMessage, ok bool, field string) error {
	if !ok {
		return nil
	}

	var value []json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s must be an array: %w", field, err)
	}
	_ = value
	return nil
}

func decodeEventOp(kind string) string {
	switch kind {
	case "book":
		return "market.decode_book"
	case "price_change":
		return "market.decode_price_change"
	case "best_bid_ask":
		return "market.decode_best_bid_ask"
	default:
		return "market.decode"
	}
}

func inferMessageType(raw []byte) (string, error) {
	events, err := DecodeEvents(raw)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	switch {
	case events[0].Book != nil:
		return "book", nil
	case events[0].PriceChange != nil:
		return "price_change", nil
	case events[0].BestBidAsk != nil:
		return "best_bid_ask", nil
	default:
		return "", nil
	}
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
