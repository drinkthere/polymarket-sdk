package market

import (
	"encoding/json"

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
		var meta struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal(item, &meta); err != nil {
			return nil, decodeError("market.decode", item, err)
		}

		switch meta.EventType {
		case "book":
			var event BookEvent
			if err := json.Unmarshal(item, &event); err != nil {
				return nil, decodeError("market.decode_book", item, err)
			}
			events = append(events, Event{Book: &event})
		case "price_change":
			var event PriceChangeEvent
			if err := json.Unmarshal(item, &event); err != nil {
				return nil, decodeError("market.decode_price_change", item, err)
			}
			events = append(events, Event{PriceChange: &event})
		case "best_bid_ask":
			var event BestBidAskEvent
			if err := json.Unmarshal(item, &event); err != nil {
				return nil, decodeError("market.decode_best_bid_ask", item, err)
			}
			events = append(events, Event{BestBidAsk: &event})
		}
	}
	return events, nil
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
