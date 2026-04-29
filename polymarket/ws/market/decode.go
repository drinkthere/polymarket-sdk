package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

type Event struct {
	Book           *BookEvent
	PriceChange    *PriceChangeEvent
	BestBidAsk     *BestBidAskEvent
	LastTradePrice *LastTradePriceEvent
	TickSizeChange *TickSizeChangeEvent
	NewMarket      *NewMarketEvent
	MarketResolved *MarketResolvedEvent
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

type LastTradePriceEvent struct {
	EventType  string `json:"event_type"`
	Market     string `json:"market"`
	AssetID    string `json:"asset_id"`
	Price      string `json:"price"`
	Side       string `json:"side"`
	Size       string `json:"size"`
	FeeRateBps string `json:"fee_rate_bps"`
	Timestamp  string `json:"timestamp"`
}

type TickSizeChangeEvent struct {
	EventType   string `json:"event_type"`
	Market      string `json:"market"`
	AssetID     string `json:"asset_id"`
	OldTickSize string `json:"old_tick_size"`
	NewTickSize string `json:"new_tick_size"`
	Timestamp   string `json:"timestamp"`
}

type MarketEventMessage struct {
	ID          string `json:"id"`
	Ticker      string `json:"ticker"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type MarketFeeSchedule struct {
	Exponent   string `json:"exponent"`
	Rate       string `json:"rate"`
	TakerOnly  bool   `json:"taker_only"`
	RebateRate string `json:"rebate_rate"`
}

type NewMarketEvent struct {
	EventType             string             `json:"event_type"`
	ID                    string             `json:"id"`
	Question              string             `json:"question"`
	Market                string             `json:"market"`
	Slug                  string             `json:"slug"`
	Description           string             `json:"description"`
	AssetsIDs             []string           `json:"assets_ids"`
	Outcomes              []string           `json:"outcomes"`
	EventMessage          MarketEventMessage `json:"event_message"`
	Timestamp             string             `json:"timestamp"`
	Tags                  []string           `json:"tags"`
	ConditionID           string             `json:"condition_id"`
	Active                bool               `json:"active"`
	ClobTokenIDs          []string           `json:"clob_token_ids"`
	SportsMarketType      string             `json:"sports_market_type"`
	Line                  string             `json:"line"`
	GameStartTime         string             `json:"game_start_time"`
	OrderPriceMinTickSize string             `json:"order_price_min_tick_size"`
	GroupItemTitle        string             `json:"group_item_title"`
	TakerBaseFee          string             `json:"taker_base_fee"`
	FeesEnabled           bool               `json:"fees_enabled"`
	FeeSchedule           MarketFeeSchedule  `json:"fee_schedule"`
}

type MarketResolvedEvent struct {
	EventType      string             `json:"event_type"`
	ID             string             `json:"id"`
	Question       string             `json:"question"`
	Market         string             `json:"market"`
	Slug           string             `json:"slug"`
	Description    string             `json:"description"`
	AssetsIDs      []string           `json:"assets_ids"`
	Outcomes       []string           `json:"outcomes"`
	WinningAssetID string             `json:"winning_asset_id"`
	WinningOutcome string             `json:"winning_outcome"`
	EventMessage   MarketEventMessage `json:"event_message"`
	Timestamp      string             `json:"timestamp"`
}

func DecodeEvents(raw []byte) ([]Event, error) {
	batch, err := splitMessageBatch(raw)
	if err != nil {
		return nil, decodeError("market.decode", raw, err)
	}
	return decodeEventBatch(batch, true)
}

func splitMessageBatch(raw []byte) ([]json.RawMessage, error) {
	if isRawHeartbeat(raw) {
		return nil, nil
	}

	var batch []json.RawMessage
	if err := json.Unmarshal(raw, &batch); err != nil {
		var single json.RawMessage
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			return nil, err2
		}
		batch = []json.RawMessage{single}
	}
	return batch, nil
}

func decodeEventBatch(batch []json.RawMessage, strictUnknown bool) ([]Event, error) {
	events := make([]Event, 0, len(batch))
	for _, item := range batch {
		var meta eventMeta
		if err := json.Unmarshal(item, &meta); err != nil {
			return nil, decodeError("market.decode", item, err)
		}

		kind, shouldDecode, err := resolveEventKind(meta, item)
		if err != nil {
			return nil, decodeError(decodeEventOp(kind), item, err)
		}
		if !shouldDecode {
			continue
		}

		switch kind {
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
		case "last_trade_price":
			event, err := decodeLastTradePriceEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_last_trade_price", item, err)
			}
			events = append(events, Event{LastTradePrice: &event})
		case "tick_size_change":
			event, err := decodeTickSizeChangeEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_tick_size_change", item, err)
			}
			events = append(events, Event{TickSizeChange: &event})
		case "new_market":
			event, err := decodeNewMarketEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_new_market", item, err)
			}
			events = append(events, Event{NewMarket: &event})
		case "market_resolved":
			event, err := decodeMarketResolvedEvent(item)
			if err != nil {
				return nil, decodeError("market.decode_market_resolved", item, err)
			}
			events = append(events, Event{MarketResolved: &event})
		default:
			if strictUnknown && !meta.isFallback() {
				return nil, decodeError("market.decode", item, fmt.Errorf("unsupported event_type %q", kind))
			}
		}
	}
	return events, nil
}

func isRawHeartbeat(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("PONG"))
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

func resolveEventKind(meta eventMeta, raw []byte) (string, bool, error) {
	kind := meta.kind()
	if kind == "" {
		return classifyImplicitMarketEvent(raw)
	}
	if !meta.isFallback() {
		return kind, true, nil
	}

	shouldDecode, err := classifyLegacyMarketEvent(kind, raw)
	return kind, shouldDecode, err
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
	case "last_trade_price":
		assetID, hasAssetID := payload["asset_id"]
		price, hasPrice := payload["price"]
		if !hasAssetID && !hasPrice {
			return false, nil
		}
		if err := requireNonEmptyJSONString(assetID, hasAssetID, "asset_id"); err != nil {
			return false, err
		}
		if err := requireJSONString(price, hasPrice, "price"); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func classifyImplicitMarketEvent(raw []byte) (string, bool, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false, err
	}

	_, hasAssetID := payload["asset_id"]
	bids, hasBids := payload["bids"]
	asks, hasAsks := payload["asks"]
	if !hasAssetID && !hasBids && !hasAsks {
		return "", false, nil
	}
	if !hasBids && !hasAsks {
		return "", false, nil
	}
	if err := requireNonEmptyJSONString(payload["asset_id"], hasAssetID, "asset_id"); err != nil {
		return "book", false, err
	}
	if err := requireJSONArray(bids, hasBids, "bids"); err != nil {
		return "book", false, err
	}
	if err := requireJSONArray(asks, hasAsks, "asks"); err != nil {
		return "book", false, err
	}
	return "book", true, nil
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
	case "last_trade_price":
		return "market.decode_last_trade_price"
	case "tick_size_change":
		return "market.decode_tick_size_change"
	case "new_market":
		return "market.decode_new_market"
	case "market_resolved":
		return "market.decode_market_resolved"
	default:
		return "market.decode"
	}
}

func inferMessageType(raw []byte) (string, error) {
	batch, err := splitMessageBatch(raw)
	if err != nil {
		return "", decodeError("market.decode", raw, err)
	}
	if len(batch) == 0 {
		return "", nil
	}

	events, err := decodeEventBatch(batch, false)
	if err != nil {
		return "", err
	}
	if len(events) == 0 || len(events) != len(batch) {
		return "", nil
	}
	kind := eventKind(events[0])
	if kind == "" {
		return "", nil
	}
	for _, event := range events[1:] {
		if eventKind(event) != kind {
			return "", nil
		}
	}
	return kind, nil
}

func eventKind(event Event) string {
	switch {
	case event.Book != nil:
		return "book"
	case event.PriceChange != nil:
		return "price_change"
	case event.BestBidAsk != nil:
		return "best_bid_ask"
	case event.LastTradePrice != nil:
		return "last_trade_price"
	case event.TickSizeChange != nil:
		return "tick_size_change"
	case event.NewMarket != nil:
		return "new_market"
	case event.MarketResolved != nil:
		return "market_resolved"
	default:
		return ""
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

	eventType := normalizedEventType(payload.kind(), "book")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return BookEvent{}, err
	}

	return BookEvent{
		EventType: eventType,
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

	eventType := normalizedEventType(payload.kind(), "price_change")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return PriceChangeEvent{}, err
	}

	return PriceChangeEvent{
		EventType:    eventType,
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

	eventType := normalizedEventType(payload.kind(), "best_bid_ask")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return BestBidAskEvent{}, err
	}

	return BestBidAskEvent{
		EventType: eventType,
		Market:    payload.Market,
		AssetID:   payload.AssetID,
		BestBid:   payload.BestBid,
		BestAsk:   payload.BestAsk,
		Spread:    payload.Spread,
		Timestamp: timestamp,
	}, nil
}

func decodeLastTradePriceEvent(raw []byte) (LastTradePriceEvent, error) {
	var payload struct {
		eventMeta
		Market     string          `json:"market"`
		AssetID    string          `json:"asset_id"`
		Price      string          `json:"price"`
		Side       string          `json:"side"`
		Size       string          `json:"size"`
		FeeRateBps string          `json:"fee_rate_bps"`
		Timestamp  json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return LastTradePriceEvent{}, err
	}

	eventType := normalizedEventType(payload.kind(), "last_trade_price")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return LastTradePriceEvent{}, err
	}

	return LastTradePriceEvent{
		EventType:  eventType,
		Market:     payload.Market,
		AssetID:    payload.AssetID,
		Price:      payload.Price,
		Side:       payload.Side,
		Size:       payload.Size,
		FeeRateBps: payload.FeeRateBps,
		Timestamp:  timestamp,
	}, nil
}

func decodeTickSizeChangeEvent(raw []byte) (TickSizeChangeEvent, error) {
	var payload struct {
		eventMeta
		Market      string          `json:"market"`
		AssetID     string          `json:"asset_id"`
		OldTickSize string          `json:"old_tick_size"`
		NewTickSize string          `json:"new_tick_size"`
		Timestamp   json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return TickSizeChangeEvent{}, err
	}

	eventType := normalizedEventType(payload.kind(), "tick_size_change")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return TickSizeChangeEvent{}, err
	}

	return TickSizeChangeEvent{
		EventType:   eventType,
		Market:      payload.Market,
		AssetID:     payload.AssetID,
		OldTickSize: payload.OldTickSize,
		NewTickSize: payload.NewTickSize,
		Timestamp:   timestamp,
	}, nil
}

func decodeNewMarketEvent(raw []byte) (NewMarketEvent, error) {
	var payload struct {
		eventMeta
		ID                    string             `json:"id"`
		Question              string             `json:"question"`
		Market                string             `json:"market"`
		Slug                  string             `json:"slug"`
		Description           string             `json:"description"`
		AssetsIDs             []string           `json:"assets_ids"`
		Outcomes              []string           `json:"outcomes"`
		EventMessage          MarketEventMessage `json:"event_message"`
		Timestamp             json.RawMessage    `json:"timestamp"`
		Tags                  []string           `json:"tags"`
		ConditionID           string             `json:"condition_id"`
		Active                bool               `json:"active"`
		ClobTokenIDs          []string           `json:"clob_token_ids"`
		SportsMarketType      string             `json:"sports_market_type"`
		Line                  string             `json:"line"`
		GameStartTime         string             `json:"game_start_time"`
		OrderPriceMinTickSize string             `json:"order_price_min_tick_size"`
		GroupItemTitle        string             `json:"group_item_title"`
		TakerBaseFee          string             `json:"taker_base_fee"`
		FeesEnabled           bool               `json:"fees_enabled"`
		FeeSchedule           MarketFeeSchedule  `json:"fee_schedule"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return NewMarketEvent{}, err
	}

	eventType := normalizedEventType(payload.kind(), "new_market")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return NewMarketEvent{}, err
	}

	return NewMarketEvent{
		EventType:             eventType,
		ID:                    payload.ID,
		Question:              payload.Question,
		Market:                payload.Market,
		Slug:                  payload.Slug,
		Description:           payload.Description,
		AssetsIDs:             payload.AssetsIDs,
		Outcomes:              payload.Outcomes,
		EventMessage:          payload.EventMessage,
		Timestamp:             timestamp,
		Tags:                  payload.Tags,
		ConditionID:           payload.ConditionID,
		Active:                payload.Active,
		ClobTokenIDs:          payload.ClobTokenIDs,
		SportsMarketType:      payload.SportsMarketType,
		Line:                  payload.Line,
		GameStartTime:         payload.GameStartTime,
		OrderPriceMinTickSize: payload.OrderPriceMinTickSize,
		GroupItemTitle:        payload.GroupItemTitle,
		TakerBaseFee:          payload.TakerBaseFee,
		FeesEnabled:           payload.FeesEnabled,
		FeeSchedule:           payload.FeeSchedule,
	}, nil
}

func decodeMarketResolvedEvent(raw []byte) (MarketResolvedEvent, error) {
	var payload struct {
		eventMeta
		ID             string             `json:"id"`
		Question       string             `json:"question"`
		Market         string             `json:"market"`
		Slug           string             `json:"slug"`
		Description    string             `json:"description"`
		AssetsIDs      []string           `json:"assets_ids"`
		Outcomes       []string           `json:"outcomes"`
		WinningAssetID string             `json:"winning_asset_id"`
		WinningOutcome string             `json:"winning_outcome"`
		EventMessage   MarketEventMessage `json:"event_message"`
		Timestamp      json.RawMessage    `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return MarketResolvedEvent{}, err
	}

	eventType := normalizedEventType(payload.kind(), "market_resolved")
	timestamp, err := decodeTimestamp(payload.Timestamp)
	if err != nil {
		return MarketResolvedEvent{}, err
	}

	return MarketResolvedEvent{
		EventType:      eventType,
		ID:             payload.ID,
		Question:       payload.Question,
		Market:         payload.Market,
		Slug:           payload.Slug,
		Description:    payload.Description,
		AssetsIDs:      payload.AssetsIDs,
		Outcomes:       payload.Outcomes,
		WinningAssetID: payload.WinningAssetID,
		WinningOutcome: payload.WinningOutcome,
		EventMessage:   payload.EventMessage,
		Timestamp:      timestamp,
	}, nil
}

func normalizedEventType(kind, fallback string) string {
	if s := strings.TrimSpace(kind); s != "" {
		return s
	}
	return fallback
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
