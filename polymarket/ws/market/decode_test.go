package market

import (
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
		}
	]`)

	events, err := DecodeEvents(payload)
	if err != nil {
		t.Fatalf("DecodeEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(DecodeEvents()) = %d, want 3", len(events))
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
