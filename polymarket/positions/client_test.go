package positions

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
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
		if q.Get("user") != "0xabc" {
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
		User:          "0xabc",
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
		if q.Get("user") != "0xdef" {
			t.Fatalf("unexpected user query: %s", q.Get("user"))
		}
		if q.Get("mergeable") != "true" {
			t.Fatalf("unexpected mergeable query: %s", q.Get("mergeable"))
		}
		if q.Get("sizeThreshold") != "0" {
			t.Fatalf("unexpected sizeThreshold query: %s", q.Get("sizeThreshold"))
		}
		if q.Get("limit") != "2" {
			t.Fatalf("unexpected limit query: %s", q.Get("limit"))
		}

		w.Header().Set("Content-Type", "application/json")
		switch q.Get("offset") {
		case "1":
			_, _ = io.WriteString(w, `[{"asset":"tok-1","conditionId":"cond-1","size":1,"avgPrice":0.10,"title":"A","outcome":"Yes","side":"BUY","negativeRisk":false,"outcomeIndex":0},{"asset":"tok-2","conditionId":"cond-2","size":2,"avgPrice":0.20,"title":"B","outcome":"No","side":"SELL","negativeRisk":true,"outcomeIndex":1}]`)
		case "3":
			_, _ = io.WriteString(w, `[{"asset":"tok-3","conditionId":"cond-3","size":3,"avgPrice":0.30,"title":"C","outcome":"Yes","side":"BUY","negativeRisk":false,"outcomeIndex":0}]`)
		default:
			t.Fatalf("unexpected offset query: %s", q.Get("offset"))
		}
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.ListAll(context.Background(), ListRequest{
		User:          "0xdef",
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
	if len(resp.Positions) != 3 {
		t.Fatalf("len(resp.Positions) = %d, want 3", len(resp.Positions))
	}
	if resp.Positions[0].Asset != "tok-1" || resp.Positions[1].Asset != "tok-2" || resp.Positions[2].Asset != "tok-3" {
		t.Fatalf("unexpected position order: %+v", resp.Positions)
	}
}

func TestClientListAllDefaultsLimitTo500(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("user"); got != "0xlimit" {
			t.Fatalf("user query = %s, want 0xlimit", got)
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

	resp, err := client.ListAll(context.Background(), ListRequest{User: "0xlimit"})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(resp.Positions) != 0 {
		t.Fatalf("len(resp.Positions) = %d, want 0", len(resp.Positions))
	}
}

func TestClientListAllNegativeLimitDefaultsTo500(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("user"); got != "0xlimit-neg" {
			t.Fatalf("user query = %s, want 0xlimit-neg", got)
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

	resp, err := client.ListAll(context.Background(), ListRequest{User: "0xlimit-neg", Limit: -1})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(resp.Positions) != 0 {
		t.Fatalf("len(resp.Positions) = %d, want 0", len(resp.Positions))
	}
}

func TestClientListAllReturnsTypedRequestBuildErrorBeforeOffsetExceedsCeiling(t *testing.T) {
	requests := 0
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("offset"); got != "10000" {
			t.Fatalf("offset query = %s, want 10000", got)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Fatalf("limit query = %s, want 2", got)
		}
		if got := r.URL.Query().Get("sizeThreshold"); got != "0" {
			t.Fatalf("sizeThreshold query = %s, want 0", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"asset":"tok-1","conditionId":"cond-1","size":1,"avgPrice":0.10,"title":"A","outcome":"Yes","side":"BUY","negativeRisk":false,"outcomeIndex":0},{"asset":"tok-2","conditionId":"cond-2","size":2,"avgPrice":0.20,"title":"B","outcome":"No","side":"SELL","negativeRisk":true,"outcomeIndex":1}]`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListAll(context.Background(), ListRequest{
		User:   "0xcap",
		Limit:  2,
		Offset: 10000,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
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

func TestClientListReturnsTypedDecodeError(t *testing.T) {
	httpClient := newHTTPClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{`)
	}))

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.List(context.Background(), ListRequest{User: "0xdecode"})
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

func TestClientListRequiresUser(t *testing.T) {
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
