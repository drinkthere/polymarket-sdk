package market

import (
	"strings"
	"testing"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

func TestDecodeEventsBatch(t *testing.T) {
	t.Parallel()

	payload := []byte(`[
		{
			"event_type":"book",
			"asset_id":"65818619657568813474341868652308942079804919287380422192892211131408793125422",
			"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
			"bids":[
				{"price":".48","size":"30"},
				{"price":".49","size":"20"}
			],
			"asks":[
				{"price":".52","size":"25"},
				{"price":".53","size":"60"}
			],
			"timestamp":"123456789000",
			"hash":"0x0...."
		},
		{
			"market":"0x5f65177b394277fd294cd75650044e32ba009a95022d88a0c1d565897d72f8f1",
			"price_changes":[
				{
					"asset_id":"71321045679252212594626385532706912750332728571942532289631379312455583992563",
					"price":"0.5",
					"size":"200",
					"side":"BUY",
					"hash":"56621a121a47ed9333273e21c83b660cff37ae50",
					"best_bid":"0.5",
					"best_ask":"1"
				}
			],
			"timestamp":"1757908892351",
			"event_type":"price_change"
		},
		{
			"event_type":"best_bid_ask",
			"market":"0x0005c0d312de0be897668695bae9f32b624b4a1ae8b140c49f08447fcc74f442",
			"asset_id":"85354956062430465315924116860125388538595433819574542752031640332592237464430",
			"best_bid":"0.73",
			"best_ask":"0.77",
			"spread":"0.04",
			"timestamp":"1766789469958"
		}
	]`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(DecodeEvents()) = %d, want 3", len(events))
	}
	if events[0].Book == nil || events[0].Book.AssetID != "65818619657568813474341868652308942079804919287380422192892211131408793125422" {
		t.Fatalf("unexpected book event: %+v", events[0])
	}
	if len(events[0].Book.Bids) != 2 || events[0].Book.Bids[0].Price != ".48" {
		t.Fatalf("unexpected book bids: %+v", events[0].Book.Bids)
	}
	if events[1].PriceChange == nil || len(events[1].PriceChange.PriceChanges) != 1 {
		t.Fatalf("unexpected price_change event: %+v", events[1])
	}
	if got := events[1].PriceChange.PriceChanges[0]; got.Side != "BUY" || got.BestAsk != "1" {
		t.Fatalf("unexpected price_change payload: %+v", got)
	}
	if events[2].BestBidAsk == nil || events[2].BestBidAsk.Spread != "0.04" {
		t.Fatalf("unexpected best_bid_ask event: %+v", events[2])
	}
}

func TestDecodeEventsSinglePayload(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"event_type":"best_bid_ask",
		"market":"0x0005c0d312de0be897668695bae9f32b624b4a1ae8b140c49f08447fcc74f442",
		"asset_id":"85354956062430465315924116860125388538595433819574542752031640332592237464430",
		"best_bid":"0.73",
		"best_ask":"0.77",
		"spread":"0.04",
		"timestamp":"1766789469958"
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].BestBidAsk == nil || events[0].BestBidAsk.AssetID == "" {
		t.Fatalf("unexpected best_bid_ask event: %+v", events[0])
	}
}

func TestDecodeEventsAcceptsEventFallback(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"event":"book",
		"asset_id":"65818619657568813474341868652308942079804919287380422192892211131408793125422",
		"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
		"bids":[{"price":".48","size":"30"}],
		"asks":[{"price":".52","size":"25"}],
		"timestamp":"123456789000",
		"hash":"0x0...."
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].Book == nil {
		t.Fatalf("expected book event, got %+v", events[0])
	}
	if events[0].Book.EventType != "book" {
		t.Fatalf("book event type = %q, want book", events[0].Book.EventType)
	}
}

func TestDecodeEventsAcceptsBookSnapshotWithoutEventType(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"asset_id":"65818619657568813474341868652308942079804919287380422192892211131408793125422",
		"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
		"bids":[{"price":".48","size":"30"}],
		"asks":[{"price":".52","size":"25"}],
		"timestamp":"123456789000",
		"hash":"0x0...."
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].Book == nil {
		t.Fatalf("expected book event, got %+v", events[0])
	}
	if events[0].Book.EventType != "book" {
		t.Fatalf("book event type = %q, want book", events[0].Book.EventType)
	}
}

func TestDecodeEventsDecodesLastTradePrice(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"event_type":"last_trade_price",
		"asset_id":"token-yes",
		"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
		"fee_rate_bps":"0",
		"price":"0.61",
		"side":"BUY",
		"size":"219.217767",
		"timestamp":"1700000000000"
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].LastTradePrice == nil {
		t.Fatalf("expected last_trade_price event, got %+v", events[0])
	}
	if got := events[0].LastTradePrice; got.AssetID != "token-yes" || got.Price != "0.61" || got.Side != "BUY" || got.Size != "219.217767" || got.FeeRateBps != "0" || got.Timestamp != "1700000000000" {
		t.Fatalf("unexpected last_trade_price payload: %+v", got)
	}
}

func TestDecodeEventsDecodesTickSizeChange(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"event_type":"tick_size_change",
		"asset_id":"65818619657568813474341868652308942079804919287380422192892211131408793125422",
		"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
		"old_tick_size":"0.01",
		"new_tick_size":"0.001",
		"timestamp":"100000000"
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].TickSizeChange == nil {
		t.Fatalf("expected tick_size_change event, got %+v", events[0])
	}
	if got := events[0].TickSizeChange; got.AssetID == "" || got.OldTickSize != "0.01" || got.NewTickSize != "0.001" || got.Timestamp != "100000000" {
		t.Fatalf("unexpected tick_size_change payload: %+v", got)
	}
}

func TestDecodeEventsDecodesNewMarket(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"id":"1031769",
		"question":"Will NVIDIA (NVDA) close above $240 end of January?",
		"market":"0x311d0c4b6671ab54af4970c06fcf58662516f5168997bdda209ec3db5aa6b0c1",
		"slug":"nvda-above-240-on-january-30-2026",
		"description":"This market will resolve to \"Yes\" if the official closing price...",
		"assets_ids":[
			"76043073756653678226373981964075571318267289248134717369284518995922789326425",
			"31690934263385727664202099278545688007799199447969475608906331829650099442770"
		],
		"outcomes":["Yes","No"],
		"event_message":{
			"id":"125819",
			"ticker":"nvda-above-in-january-2026",
			"slug":"nvda-above-in-january-2026",
			"title":"Will NVIDIA (NVDA) close above ___ end of January?",
			"description":"This market will resolve to \"Yes\" if the official closing price..."
		},
		"timestamp":"1766790415550",
		"event_type":"new_market",
		"tags":["stocks"],
		"condition_id":"0x311d0c4b6671ab54af4970c06fcf58662516f5168997bdda209ec3db5aa6b0c1",
		"active":true,
		"clob_token_ids":[
			"76043073756653678226373981964075571318267289248134717369284518995922789326425",
			"31690934263385727664202099278545688007799199447969475608906331829650099442770"
		],
		"sports_market_type":"",
		"line":"",
		"game_start_time":"",
		"order_price_min_tick_size":"0.01",
		"group_item_title":"NVDA above $240",
		"taker_base_fee":"0",
		"fees_enabled":true,
		"fee_schedule":{
			"exponent":"2",
			"rate":"0.02",
			"taker_only":true,
			"rebate_rate":"0"
		}
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].NewMarket == nil {
		t.Fatalf("expected new_market event, got %+v", events[0])
	}
	if got := events[0].NewMarket; got.ID != "1031769" || len(got.AssetsIDs) != 2 || got.EventMessage.Ticker != "nvda-above-in-january-2026" || got.FeeSchedule.Rate != "0.02" || !got.FeesEnabled {
		t.Fatalf("unexpected new_market payload: %+v", got)
	}
}

func TestDecodeEventsDecodesMarketResolved(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"id":"1031769",
		"question":"Will NVIDIA (NVDA) close above $240 end of January?",
		"market":"0x311d0c4b6671ab54af4970c06fcf58662516f5168997bdda209ec3db5aa6b0c1",
		"slug":"nvda-above-240-on-january-30-2026",
		"description":"This market will resolve to \"Yes\" if the official closing price...",
		"assets_ids":[
			"76043073756653678226373981964075571318267289248134717369284518995922789326425",
			"31690934263385727664202099278545688007799199447969475608906331829650099442770"
		],
		"outcomes":["Yes","No"],
		"winning_asset_id":"76043073756653678226373981964075571318267289248134717369284518995922789326425",
		"winning_outcome":"Yes",
		"event_message":{
			"id":"125819",
			"ticker":"nvda-above-in-january-2026",
			"slug":"nvda-above-in-january-2026",
			"title":"Will NVIDIA (NVDA) close above ___ end of January?",
			"description":"This market will resolve to \"Yes\" if the official closing price..."
		},
		"timestamp":"1766790415550",
		"event_type":"market_resolved"
	}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(DecodeEvents()) = %d, want 1", len(events))
	}
	if events[0].MarketResolved == nil {
		t.Fatalf("expected market_resolved event, got %+v", events[0])
	}
	if got := events[0].MarketResolved; got.ID != "1031769" || got.WinningAssetID == "" || got.WinningOutcome != "Yes" || got.EventMessage.Ticker != "nvda-above-in-january-2026" {
		t.Fatalf("unexpected market_resolved payload: %+v", got)
	}
}

func TestDecodeEventsAcceptsNumericTimestamps(t *testing.T) {
	t.Parallel()

	payload := []byte(`[
		{
			"event_type":"book",
			"asset_id":"65818619657568813474341868652308942079804919287380422192892211131408793125422",
			"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
			"bids":[{"price":".48","size":"30"}],
			"asks":[{"price":".52","size":"25"}],
			"timestamp":123456789000,
			"hash":"0x0...."
		},
		{
			"market":"0x5f65177b394277fd294cd75650044e32ba009a95022d88a0c1d565897d72f8f1",
			"price_changes":[
				{
					"asset_id":"71321045679252212594626385532706912750332728571942532289631379312455583992563",
					"price":"0.5",
					"size":"200",
					"side":"BUY",
					"hash":"56621a121a47ed9333273e21c83b660cff37ae50",
					"best_bid":"0.5",
					"best_ask":"1"
				}
			],
			"timestamp":1757908892351,
			"event_type":"price_change"
		},
		{
			"event_type":"best_bid_ask",
			"market":"0x0005c0d312de0be897668695bae9f32b624b4a1ae8b140c49f08447fcc74f442",
			"asset_id":"85354956062430465315924116860125388538595433819574542752031640332592237464430",
			"best_bid":"0.73",
			"best_ask":"0.77",
			"spread":"0.04",
			"timestamp":1766789469958
		},
		{
			"event_type":"last_trade_price",
			"asset_id":"token-yes",
			"market":"0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af",
			"price":"0.61",
			"timestamp":1700000000000
		}
	]`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("len(DecodeEvents()) = %d, want 4", len(events))
	}
	if events[0].Book == nil || events[0].Book.Timestamp != "123456789000" {
		t.Fatalf("unexpected numeric book timestamp decode: %+v", events[0])
	}
	if events[1].PriceChange == nil || events[1].PriceChange.Timestamp != "1757908892351" {
		t.Fatalf("unexpected numeric price_change timestamp decode: %+v", events[1])
	}
	if events[2].BestBidAsk == nil || events[2].BestBidAsk.Timestamp != "1766789469958" {
		t.Fatalf("unexpected numeric best_bid_ask timestamp decode: %+v", events[2])
	}
	if events[3].LastTradePrice == nil || events[3].LastTradePrice.Timestamp != "1700000000000" {
		t.Fatalf("unexpected numeric last_trade_price timestamp decode: %+v", events[3])
	}
}

func TestDecodeEventsIgnoresControlFrameEventFallback(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event":"book","assets_ids":["token_yes"]}`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("len(DecodeEvents()) = %d, want 0 for control frame", len(events))
	}
}

func TestDecodeEventsIgnoresRawPONGHeartbeat(t *testing.T) {
	t.Parallel()

	events, err := DecodeEvents([]byte("PONG"))
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("len(DecodeEvents()) = %d, want 0 for raw heartbeat", len(events))
	}
}

func TestDecodeEventsMalformedLegacyEventFallbackReturnsTypedDecodeError(t *testing.T) {
	t.Parallel()

	_, err := DecodeEvents([]byte(`{"event":"book","asset_id":"1","bids":"oops"}`))
	if err == nil {
		t.Fatal("DecodeEvents() error = nil, want typed decode error")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("DecodeEvents() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "market.decode_book" {
		t.Fatalf("expected decode_book op, got %q", typed.Op)
	}
	if len(typed.RawBody) == 0 {
		t.Fatal("expected raw body on typed decode error")
	}
}

func TestDecodeEventsReturnsTypedDecodeError(t *testing.T) {
	t.Parallel()

	_, err := DecodeEvents([]byte(`{"event_type":"book","bids":"oops"}`))
	if err == nil {
		t.Fatal("DecodeEvents() error = nil, want typed decode error")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("DecodeEvents() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "market.decode_book" {
		t.Fatalf("expected decode_book op, got %q", typed.Op)
	}
	if len(typed.RawBody) == 0 {
		t.Fatal("expected raw body on typed decode error")
	}
}

func TestDecodeEventsKnownAndUnknownMixedBatchReturnsTypedDecodeError(t *testing.T) {
	t.Parallel()

	_, err := DecodeEvents([]byte(`[
		{
			"event_type":"book",
			"asset_id":"token-yes",
			"bids":[{"price":"0.48","size":"30"}],
			"asks":[{"price":"0.52","size":"25"}],
			"timestamp":"1700000000000"
		},
		{
			"event_type":"mystery_event",
			"asset_id":"token-no",
			"timestamp":"1700000000100"
		}
	]`))
	if err == nil {
		t.Fatal("DecodeEvents() error = nil, want typed decode error for mixed known+unknown batch")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("DecodeEvents() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "market.decode" {
		t.Fatalf("expected market.decode op, got %q", typed.Op)
	}
	if !strings.Contains(string(typed.RawBody), `"event_type":"mystery_event"`) {
		t.Fatalf("expected raw body for unknown item, got %q", string(typed.RawBody))
	}
}

func TestDecodeEventsExplicitUnknownEventTypeReturnsTypedDecodeError(t *testing.T) {
	t.Parallel()

	_, err := DecodeEvents([]byte(`{
		"event_type":"mystery_event",
		"asset_id":"token-no",
		"timestamp":"1700000000100"
	}`))
	if err == nil {
		t.Fatal("DecodeEvents() error = nil, want typed decode error for unknown event_type")
	}

	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("DecodeEvents() error type = %T, want *errors.Error", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "market.decode" {
		t.Fatalf("expected market.decode op, got %q", typed.Op)
	}
	if len(typed.RawBody) == 0 {
		t.Fatal("expected raw body on typed decode error")
	}
}

func TestInferMessageTypeRecognizesDocumentedMarketKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{
			name: "tick_size_change",
			payload: []byte(`{
				"event_type":"tick_size_change",
				"asset_id":"token-yes",
				"old_tick_size":"0.01",
				"new_tick_size":"0.001",
				"timestamp":"100000000"
			}`),
			want: "tick_size_change",
		},
		{
			name: "new_market",
			payload: []byte(`{
				"event_type":"new_market",
				"id":"1031769",
				"assets_ids":["yes","no"],
				"outcomes":["Yes","No"],
				"event_message":{"ticker":"nvda"},
				"timestamp":"1766790415550"
			}`),
			want: "new_market",
		},
		{
			name: "market_resolved",
			payload: []byte(`{
				"event_type":"market_resolved",
				"id":"1031769",
				"assets_ids":["yes","no"],
				"winning_asset_id":"yes",
				"winning_outcome":"Yes",
				"timestamp":"1766790415550"
			}`),
			want: "market_resolved",
		},
		{
			name: "last_trade_price",
			payload: []byte(`{
				"event_type":"last_trade_price",
				"asset_id":"token-yes",
				"fee_rate_bps":"0",
				"price":"0.61",
				"side":"BUY",
				"size":"219.217767",
				"timestamp":"1700000000000"
			}`),
			want: "last_trade_price",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := inferMessageType(tt.payload)
			if err != nil {
				t.Fatalf("inferMessageType() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("inferMessageType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferMessageTypeReturnsEmptyForKnownAndUnknownMixedBatch(t *testing.T) {
	t.Parallel()

	payload := []byte(`[
		{
			"event_type":"book",
			"asset_id":"token-yes",
			"bids":[{"price":"0.48","size":"30"}],
			"asks":[{"price":"0.52","size":"25"}],
			"timestamp":"1700000000000"
		},
		{
			"event_type":"mystery_event",
			"asset_id":"token-no",
			"timestamp":"1700000000100"
		}
	]`)

	got, err := inferMessageType(payload)
	if err != nil {
		t.Fatalf("inferMessageType() error = %v", err)
	}
	if got != "" {
		t.Fatalf("inferMessageType() = %q, want empty for known+unknown mixed batch", got)
	}
}
