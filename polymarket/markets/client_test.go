package markets

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

func TestClientListMarketsBuildsRequestAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/markets" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("active") != "true" || q.Get("closed") != "false" {
			t.Fatalf("unexpected active/closed query: %s", q.Encode())
		}
		if q.Get("limit") != "1000" || q.Get("offset") != "2000" {
			t.Fatalf("unexpected pagination query: %s", q.Encode())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"pm-1","conditionId":"cond-1","slug":"btc-updown-5m-1","question":"BTC?","description":"desc","outcomes":["Yes","No"],"outcomePrices":["0.52","0.48"],"clobTokenIds":["tok-yes","tok-no"],"eventStartTime":"2026-04-14T13:00:00Z","endDate":"2026-04-14T13:05:00Z","createdAt":"2026-04-14T12:59:00Z","active":true,"closed":false,"bestBid":0.51,"bestAsk":0.53,"spread":0.02}]`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	resp, err := client.ListMarkets(context.Background(), ListRequest{Active: true, Closed: false, Limit: 1000, Offset: 2000})
	if err != nil {
		t.Fatalf("ListMarkets() error: %v", err)
	}
	if len(resp.Markets) != 1 {
		t.Fatalf("expected 1 market, got %d", len(resp.Markets))
	}
	if resp.Markets[0].ID != "pm-1" || resp.Markets[0].Slug != "btc-updown-5m-1" {
		t.Fatalf("unexpected market: %+v", resp.Markets[0])
	}
	if resp.Markets[0].ConditionID != "cond-1" || resp.Markets[0].BestBid != 0.51 || resp.Markets[0].BestAsk != 0.53 || resp.Markets[0].Spread != 0.02 {
		t.Fatalf("unexpected market fields: %+v", resp.Markets[0])
	}
	if string(resp.Markets[0].OutcomePrices) != `["0.52","0.48"]` {
		t.Fatalf("unexpected outcome prices: %s", string(resp.Markets[0].OutcomePrices))
	}
}

func TestClientGetEventsBySlugBuildsRequestAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("slug"); got != "btc-updown-5m-1776171600" {
			t.Fatalf("unexpected slug query: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"evt-1","slug":"btc-updown-5m-1776171600","title":"BTC Up or Down","startTime":"2026-04-14T13:00:00Z","creationDate":"2026-04-14T12:59:00Z","endDate":"2026-04-14T13:05:00Z","markets":[{"id":"pm-1","conditionId":"cond-1","slug":"btc-updown-5m-1776171600","question":"BTC?","outcomes":"[\"Yes\",\"No\"]","clobTokenIds":"[\"tok-yes\",\"tok-no\"]","eventStartTime":"2026-04-14T13:00:00Z","endDate":"2026-04-14T13:05:00Z","createdAt":"2026-04-14T12:59:00Z"}]}]`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	resp, err := client.GetEventsBySlug(context.Background(), "btc-updown-5m-1776171600")
	if err != nil {
		t.Fatalf("GetEventsBySlug() error: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Events))
	}
	if len(resp.Events[0].Markets) != 1 {
		t.Fatalf("expected 1 nested market, got %d", len(resp.Events[0].Markets))
	}
	if resp.Events[0].Markets[0].ConditionID != "cond-1" {
		t.Fatalf("unexpected nested market: %+v", resp.Events[0].Markets[0])
	}
}

func TestClientListMarketsReturnsTypedDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, err = client.ListMarkets(context.Background(), ListRequest{})
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
}

func TestClientGetEventsBySlugReturnsTypedProtocolErrorForMissingIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"slug":"btc-updown-5m-1776171600","markets":[{"id":"pm-1"}]}]`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, err = client.GetEventsBySlug(context.Background(), "btc-updown-5m-1776171600")
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
}

func TestClientGetClobMarketInfoBuildsRequestAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/markets/0xcond-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"condition_id":"0xcond-1","gst":"2026-04-28T12:00:00Z","r":{"min_size":100},"t":[{"t":"tok-yes","o":"Yes"},{"t":"tok-no","o":"No"}],"mos":5,"mts":0.01,"mbf":0,"tbf":125,"rfqe":false,"itode":true,"ibce":false,"fd":{"r":0.05,"e":1,"to":true},"oas":2}`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	resp, err := client.GetClobMarketInfo(context.Background(), "0xcond-1")
	if err != nil {
		t.Fatalf("GetClobMarketInfo() error: %v", err)
	}
	if resp.ConditionID != "0xcond-1" {
		t.Fatalf("unexpected condition id: %+v", resp)
	}
	if len(resp.T) != 2 || resp.T[0].TokenID != "tok-yes" || resp.T[1].Outcome != "No" {
		t.Fatalf("unexpected tokens: %+v", resp.T)
	}
	if resp.MOS != 5 || resp.MTS != 0.01 || resp.TBF != 125 {
		t.Fatalf("unexpected market info numbers: %+v", resp)
	}
	if !resp.FD.TakerOnly || resp.FD.Rate != 0.05 || resp.FD.Exponent != 1 {
		t.Fatalf("unexpected fee details: %+v", resp.FD)
	}
}

func TestClientGetClobMarketInfoRequiresConditionID(t *testing.T) {
	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: "https://clob.polymarket.com", Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, err = client.GetClobMarketInfo(context.Background(), "")
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

func TestClientGetClobMarketInfoReturnsProtocolErrorOnMissingTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"condition_id":"0xcond-1","mos":5,"mts":0.01,"fd":{"r":0.05,"e":1,"to":true}}`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("httpx.New() error: %v", err)
	}

	client, err := NewClient(httpClient)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, err = client.GetClobMarketInfo(context.Background(), "0xcond-1")
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
	if typed.Op != "markets.new" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}
