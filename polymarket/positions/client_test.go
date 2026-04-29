package positions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

const (
	validUserABC = "0x0000000000000000000000000000000000000abc"
	validUserDEF = "0x0000000000000000000000000000000000000def"
	validUser111 = "0x0000000000000000000000000000000000000111"
	validUser222 = "0x0000000000000000000000000000000000000222"
	validUser333 = "0x0000000000000000000000000000000000000333"
	validUser444 = "0x0000000000000000000000000000000000000444"
	validUser555 = "0x0000000000000000000000000000000000000555"
)

func TestClientListBuildsRequestAndDecodesResponse(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/positions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		q := r.URL.Query()
		if q.Get("user") != validUserABC {
			t.Fatalf("unexpected user query: %s", q.Get("user"))
		}
		if q.Get("redeemable") != "true" {
			t.Fatalf("unexpected redeemable query: %s", q.Get("redeemable"))
		}
		if q.Get("mergeable") != "false" {
			t.Fatalf("unexpected mergeable query: %s", q.Get("mergeable"))
		}
		if q.Get("limit") != "25" || q.Get("offset") != "50" {
			t.Fatalf("unexpected pagination query: %s", q.Encode())
		}
		if q.Get("sizeThreshold") != "0" {
			t.Fatalf("unexpected sizeThreshold query: %s", q.Get("sizeThreshold"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"asset":"tok-1","conditionId":"cond-1","size":12.5,"avgPrice":0.43,"title":"Will BTC close above $100k?","outcome":"Yes","side":"BUY","negativeRisk":false,"outcomeIndex":0}]`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.List(context.Background(), ListRequest{
		User:          validUserABC,
		Redeemable:    boolPtr(true),
		Mergeable:     boolPtr(false),
		Limit:         25,
		Offset:        50,
		SizeThreshold: "0",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(resp.Positions) != 1 {
		t.Fatalf("len(resp.Positions) = %d, want 1", len(resp.Positions))
	}

	got := resp.Positions[0]
	if got.Asset != "tok-1" || got.ConditionID != "cond-1" || got.Title != "Will BTC close above $100k?" {
		t.Fatalf("unexpected position identity fields: %+v", got)
	}
	if got.Size != 12.5 || got.AvgPrice != 0.43 || got.Outcome != "Yes" || got.Side != "BUY" {
		t.Fatalf("unexpected position numeric/text fields: %+v", got)
	}
	if got.NegativeRisk || got.OutcomeIndex != 0 {
		t.Fatalf("unexpected position flags: %+v", got)
	}
}

func TestClientListAllPaginatesUntilShortPage(t *testing.T) {
	requests := 0
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/positions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		q := r.URL.Query()
		if q.Get("user") != validUserDEF {
			t.Fatalf("unexpected user query: %s", q.Get("user"))
		}
		if q.Get("mergeable") != "true" {
			t.Fatalf("unexpected mergeable query: %s", q.Get("mergeable"))
		}
		if q.Get("sizeThreshold") != "0" {
			t.Fatalf("unexpected sizeThreshold query: %s", q.Get("sizeThreshold"))
		}
		if q.Get("limit") != "500" {
			t.Fatalf("unexpected limit query: %s", q.Get("limit"))
		}

		w.Header().Set("Content-Type", "application/json")
		switch q.Get("offset") {
		case "1":
			_, _ = io.WriteString(w, positionsJSONPage(t, 1, 500))
		case "501":
			_, _ = io.WriteString(w, positionsJSONPage(t, 501, 3))
		default:
			t.Fatalf("unexpected offset query: %s", q.Get("offset"))
		}
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.ListAll(context.Background(), ListRequest{
		User:          validUserDEF,
		Mergeable:     boolPtr(true),
		Limit:         2,
		Offset:        1,
		SizeThreshold: "0",
	})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(resp.Positions) != 503 {
		t.Fatalf("len(resp.Positions) = %d, want 503", len(resp.Positions))
	}
	if resp.Positions[0].Asset != "tok-1" || resp.Positions[499].Asset != "tok-500" || resp.Positions[500].Asset != "tok-501" || resp.Positions[len(resp.Positions)-1].Asset != "tok-503" {
		t.Fatalf("unexpected position order: %+v", resp.Positions)
	}
}

func TestClientListAllDefaultsLimitTo500(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("user"); got != validUser111 {
			t.Fatalf("user query = %s, want %s", got, validUser111)
		}
		if got := r.URL.Query().Get("sizeThreshold"); got != "0" {
			t.Fatalf("sizeThreshold query = %s, want 0", got)
		}
		if got := r.URL.Query().Get("limit"); got != "500" {
			t.Fatalf("limit query = %s, want 500", got)
		}
		if got := r.URL.Query().Get("offset"); got != "0" {
			t.Fatalf("offset query = %s, want 0", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.ListAll(context.Background(), ListRequest{User: validUser111})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(resp.Positions) != 0 {
		t.Fatalf("len(resp.Positions) = %d, want 0", len(resp.Positions))
	}
}

func TestClientListAllRejectsNegativeLimit(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListAll(context.Background(), ListRequest{User: validUser222, Limit: -1})
	if err != nil {
		var typed *polyerrors.Error
		if !errors.As(err, &typed) {
			t.Fatalf("expected *errors.Error, got %T", err)
		}
		if typed.Kind != polyerrors.ErrRequestBuild {
			t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
		}
		if typed.Op != "positions.list_all" {
			t.Fatalf("expected op positions.list_all, got %s", typed.Op)
		}
		return
	}
	t.Fatal("expected error")
}

func TestClientListAllReturnsSuccessAtMaxOffsetBoundary(t *testing.T) {
	requests := 0
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("offset"); got != fmt.Sprintf("%d", (requests-1)*500) {
			t.Fatalf("offset query = %s, want %d", got, (requests-1)*500)
		}
		if got := r.URL.Query().Get("limit"); got != "500" {
			t.Fatalf("limit query = %s, want 500", got)
		}
		if got := r.URL.Query().Get("sizeThreshold"); got != "0" {
			t.Fatalf("sizeThreshold query = %s, want 0", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, positionsJSONPage(t, (requests-1)*500, 500))
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.ListAll(context.Background(), ListRequest{
		User:  validUser333,
		Limit: 500,
	})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if requests != 21 {
		t.Fatalf("requests = %d, want 21", requests)
	}
	if len(resp.Positions) != 10500 {
		t.Fatalf("len(resp.Positions) = %d, want 10500", len(resp.Positions))
	}
	if resp.Positions[0].Asset != "tok-0" || resp.Positions[len(resp.Positions)-1].Asset != "tok-10499" {
		t.Fatalf("unexpected boundary positions: first=%+v last=%+v", resp.Positions[0], resp.Positions[len(resp.Positions)-1])
	}
}

func TestClientListAllFetchesTailPageAcrossMaxOffsetBoundary(t *testing.T) {
	var offsets []string
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)

		if got := r.URL.Query().Get("limit"); got != "500" {
			t.Fatalf("limit query = %s, want 500", got)
		}
		if got := r.URL.Query().Get("sizeThreshold"); got != "0" {
			t.Fatalf("sizeThreshold query = %s, want 0", got)
		}

		w.Header().Set("Content-Type", "application/json")
		switch offset {
		case "9800":
			_, _ = io.WriteString(w, positionsJSONPage(t, 9800, 500))
		case "10000":
			_, _ = io.WriteString(w, positionsJSONPage(t, 10000, 500))
		default:
			t.Fatalf("unexpected offset query: %s", offset)
		}
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.ListAll(context.Background(), ListRequest{
		User:   validUser333,
		Limit:  500,
		Offset: 9800,
	})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(offsets) != 2 || offsets[0] != "9800" || offsets[1] != "10000" {
		t.Fatalf("offsets = %v, want [9800 10000]", offsets)
	}
	if len(resp.Positions) != 700 {
		t.Fatalf("len(resp.Positions) = %d, want 700", len(resp.Positions))
	}
	if resp.Positions[0].Asset != "tok-9800" {
		t.Fatalf("first asset = %s, want tok-9800", resp.Positions[0].Asset)
	}
	if resp.Positions[499].Asset != "tok-10299" {
		t.Fatalf("asset[499] = %s, want tok-10299", resp.Positions[499].Asset)
	}
	if resp.Positions[500].Asset != "tok-10300" {
		t.Fatalf("asset[500] = %s, want tok-10300", resp.Positions[500].Asset)
	}
	if resp.Positions[len(resp.Positions)-1].Asset != "tok-10499" {
		t.Fatalf("last asset = %s, want tok-10499", resp.Positions[len(resp.Positions)-1].Asset)
	}
}

func TestClientListAllFetchesFullTailPageAcrossMaxOffsetBoundaryForSmallLimit(t *testing.T) {
	var offsets []string
	var limits []string
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		limits = append(limits, r.URL.Query().Get("limit"))

		if got := r.URL.Query().Get("sizeThreshold"); got != "0" {
			t.Fatalf("sizeThreshold query = %s, want 0", got)
		}

		w.Header().Set("Content-Type", "application/json")
		switch offset {
		case "9998":
			_, _ = io.WriteString(w, positionsJSONPage(t, 9998, 500))
		case "10000":
			_, _ = io.WriteString(w, positionsJSONPage(t, 10000, 500))
		default:
			t.Fatalf("unexpected offset query: %s", offset)
		}
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.ListAll(context.Background(), ListRequest{
		User:   validUser333,
		Limit:  2,
		Offset: 9998,
	})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(offsets) != 2 || offsets[0] != "9998" || offsets[1] != "10000" {
		t.Fatalf("offsets = %v, want [9998 10000]", offsets)
	}
	if len(limits) != 2 || limits[0] != "500" || limits[1] != "500" {
		t.Fatalf("limits = %v, want [500 500]", limits)
	}
	if len(resp.Positions) != 502 {
		t.Fatalf("len(resp.Positions) = %d, want 502", len(resp.Positions))
	}
	if resp.Positions[0].Asset != "tok-9998" {
		t.Fatalf("first asset = %s, want tok-9998", resp.Positions[0].Asset)
	}
	if resp.Positions[499].Asset != "tok-10497" {
		t.Fatalf("asset[499] = %s, want tok-10497", resp.Positions[499].Asset)
	}
	if resp.Positions[500].Asset != "tok-10498" {
		t.Fatalf("asset[500] = %s, want tok-10498", resp.Positions[500].Asset)
	}
	if resp.Positions[501].Asset != "tok-10499" {
		t.Fatalf("last asset = %s, want tok-10499", resp.Positions[501].Asset)
	}
}

func TestClientListReturnsTypedDecodeError(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.List(context.Background(), ListRequest{User: validUser444})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrDecode {
		t.Fatalf("expected ErrDecode, got %v", typed.Kind)
	}
	if typed.Op != "positions.list" {
		t.Fatalf("expected op positions.list, got %s", typed.Op)
	}
	if typed.Method != http.MethodGet {
		t.Fatalf("expected method GET, got %s", typed.Method)
	}
	if string(typed.RawBody) != "{" {
		t.Fatalf("unexpected raw body: %q", string(typed.RawBody))
	}
}

func TestClientListReturnsTypedProtocolErrorForInvalidDecodedPosition(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{}]`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.List(context.Background(), ListRequest{User: validUser444})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrProtocol {
		t.Fatalf("expected ErrProtocol, got %v", typed.Kind)
	}
	if typed.Op != "positions.list" {
		t.Fatalf("expected op positions.list, got %s", typed.Op)
	}
	if typed.Method != http.MethodGet {
		t.Fatalf("expected method GET, got %s", typed.Method)
	}
}

func TestClientListReturnsTypedProtocolErrorForTopLevelNull(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `null`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.List(context.Background(), ListRequest{User: validUser444})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrProtocol {
		t.Fatalf("expected ErrProtocol, got %v", typed.Kind)
	}
	if typed.Op != "positions.list" {
		t.Fatalf("expected op positions.list, got %s", typed.Op)
	}
	if typed.Method != http.MethodGet {
		t.Fatalf("expected method GET, got %s", typed.Method)
	}
}

func TestClientListReturnsTypedProtocolErrorForMissingRequiredScalarFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing_size", body: `[{"asset":"tok-1","conditionId":"cond-1","avgPrice":0.10,"negativeRisk":false,"outcomeIndex":0}]`},
		{name: "null_size", body: `[{"asset":"tok-1","conditionId":"cond-1","size":null,"avgPrice":0.10,"negativeRisk":false,"outcomeIndex":0}]`},
		{name: "missing_avg_price", body: `[{"asset":"tok-1","conditionId":"cond-1","size":1,"negativeRisk":false,"outcomeIndex":0}]`},
		{name: "missing_negative_risk", body: `[{"asset":"tok-1","conditionId":"cond-1","size":1,"avgPrice":0.10,"outcomeIndex":0}]`},
		{name: "missing_outcome_index", body: `[{"asset":"tok-1","conditionId":"cond-1","size":1,"avgPrice":0.10,"negativeRisk":false}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))

			client, err := NewClient(httpClient)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			_, err = client.List(context.Background(), ListRequest{User: validUser444})
			if err == nil {
				t.Fatal("expected error")
			}

			var typed *polyerrors.Error
			if !errors.As(err, &typed) {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if typed.Kind != polyerrors.ErrProtocol {
				t.Fatalf("expected ErrProtocol, got %v", typed.Kind)
			}
			if typed.Op != "positions.list" {
				t.Fatalf("expected op positions.list, got %s", typed.Op)
			}
		})
	}
}

func TestClientListAllReturnsTypedProtocolErrorForInvalidDecodedPage(t *testing.T) {
	requests := 0
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = io.WriteString(w, positionsJSONPage(t, 0, 500))
		case "500":
			_, _ = io.WriteString(w, `[{"conditionId":"cond-500","outcomeIndex":-1}]`)
		default:
			t.Fatalf("unexpected offset query: %s", r.URL.Query().Get("offset"))
		}
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListAll(context.Background(), ListRequest{
		User:  validUser444,
		Limit: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrProtocol {
		t.Fatalf("expected ErrProtocol, got %v", typed.Kind)
	}
	if typed.Op != "positions.list" {
		t.Fatalf("expected op positions.list, got %s", typed.Op)
	}
}

func TestClientListValidatesUser(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, tc := range []struct {
		name string
		user string
	}{
		{name: "empty", user: ""},
		{name: "whitespace", user: " \t\n "},
		{name: "invalid_hex_address", user: "0xabc"},
		{name: "missing_0x_prefix", user: "0000000000000000000000000000000000000abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.List(context.Background(), ListRequest{User: tc.user})
			if err == nil {
				t.Fatal("expected error")
			}

			var typed *polyerrors.Error
			if !errors.As(err, &typed) {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if typed.Kind != polyerrors.ErrRequestBuild {
				t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
			}
			if typed.Op != "positions.list" {
				t.Fatalf("expected op positions.list, got %s", typed.Op)
			}
		})
	}
}

func TestClientListRejectsInvalidPaginationInputs(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, tc := range []struct {
		name string
		req  ListRequest
	}{
		{name: "limit_too_large", req: ListRequest{User: validUser555, Limit: 501}},
		{name: "limit_negative", req: ListRequest{User: validUser555, Limit: -1}},
		{name: "offset_negative", req: ListRequest{User: validUser555, Offset: -1}},
		{name: "offset_too_large", req: ListRequest{User: validUser555, Offset: 10001}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.List(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected error")
			}

			var typed *polyerrors.Error
			if !errors.As(err, &typed) {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if typed.Kind != polyerrors.ErrRequestBuild {
				t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
			}
			if typed.Op != "positions.list" {
				t.Fatalf("expected op positions.list, got %s", typed.Op)
			}
		})
	}
}

func TestClientListRejectsInvalidSizeThreshold(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, tc := range []struct {
		name          string
		sizeThreshold string
	}{
		{name: "negative", sizeThreshold: "-1"},
		{name: "alpha", sizeThreshold: "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.List(context.Background(), ListRequest{
				User:          validUser555,
				SizeThreshold: tc.sizeThreshold,
			})
			if err == nil {
				t.Fatal("expected error")
			}

			var typed *polyerrors.Error
			if !errors.As(err, &typed) {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if typed.Kind != polyerrors.ErrRequestBuild {
				t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
			}
			if typed.Op != "positions.list" {
				t.Fatalf("expected op positions.list, got %s", typed.Op)
			}
		})
	}
}

func TestClientListAllRejectsLimitAbove500(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListAll(context.Background(), ListRequest{
		User:  validUser555,
		Limit: 501,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrRequestBuild {
		t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
	}
	if typed.Op != "positions.list_all" {
		t.Fatalf("expected op positions.list_all, got %s", typed.Op)
	}
}

func TestClientListAllRejectsNegativeOffsetAndInvalidSizeThreshold(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, tc := range []struct {
		name string
		req  ListRequest
	}{
		{name: "offset_negative", req: ListRequest{User: validUser555, Offset: -1}},
		{name: "offset_too_large", req: ListRequest{User: validUser555, Offset: 10001}},
		{name: "size_threshold_negative", req: ListRequest{User: validUser555, SizeThreshold: "-1"}},
		{name: "size_threshold_alpha", req: ListRequest{User: validUser555, SizeThreshold: "abc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.ListAll(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected error")
			}

			var typed *polyerrors.Error
			if !errors.As(err, &typed) {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if typed.Kind != polyerrors.ErrRequestBuild {
				t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
			}
			if typed.Op != "positions.list_all" {
				t.Fatalf("expected op positions.list_all, got %s", typed.Op)
			}
		})
	}
}

func TestNewClientNilTransportReturnsTypedRequestBuildError(t *testing.T) {
	_, err := NewClient(nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrRequestBuild {
		t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
	}
}

func newHTTPClientWithServer(t *testing.T, handler http.Handler) *httpx.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}
	return client
}

func boolPtr(v bool) *bool {
	return &v
}

type pagePosition struct {
	Asset        string  `json:"asset"`
	ConditionID  string  `json:"conditionId"`
	Size         float64 `json:"size"`
	AvgPrice     float64 `json:"avgPrice"`
	Title        string  `json:"title"`
	Outcome      string  `json:"outcome"`
	Side         string  `json:"side"`
	NegativeRisk bool    `json:"negativeRisk"`
	OutcomeIndex int     `json:"outcomeIndex"`
}

func positionsJSONPage(t *testing.T, start int, count int) string {
	t.Helper()

	page := make([]pagePosition, 0, count)
	for i := 0; i < count; i++ {
		idx := start + i
		page = append(page, pagePosition{
			Asset:        fmt.Sprintf("tok-%d", idx),
			ConditionID:  fmt.Sprintf("cond-%d", idx),
			Size:         float64(idx + 1),
			AvgPrice:     0.1,
			Title:        "A",
			Outcome:      "Yes",
			Side:         "BUY",
			NegativeRisk: false,
			OutcomeIndex: 0,
		})
	}

	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(raw)
}
