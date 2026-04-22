package gamma

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

func TestGetSettlementBySlugReturnsOutcomeYes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("slug"); got != "btc-up-only" {
			t.Fatalf("unexpected slug query: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"markets":[{"closed":true,"outcomePrices":"[\"1\",\"0\"]"}]}]`)
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

	outcome, err := client.GetSettlementBySlug(context.Background(), "btc-up-only")
	if err != nil {
		t.Fatalf("GetSettlementBySlug() error: %v", err)
	}
	if outcome != OutcomeYes {
		t.Fatalf("expected OutcomeYes, got %q", outcome)
	}
}

func TestGetSettlementBySlugReturnsOutcomeNo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("slug"); got != "btc-down-only" {
			t.Fatalf("unexpected slug query: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"markets":[{"closed":true,"outcomePrices":"[\"0\",\"1\"]"}]}]`)
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

	outcome, err := client.GetSettlementBySlug(context.Background(), "btc-down-only")
	if err != nil {
		t.Fatalf("GetSettlementBySlug() error: %v", err)
	}
	if outcome != OutcomeNo {
		t.Fatalf("expected OutcomeNo, got %q", outcome)
	}
}

func TestGetSettlementBySlugReturnsOutcomeUnsettledForOpenMarket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"markets":[{"closed":false,"outcomePrices":"[\"1\",\"0\"]"}]}]`)
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

	outcome, err := client.GetSettlementBySlug(context.Background(), "open-market")
	if err != nil {
		t.Fatalf("GetSettlementBySlug() error: %v", err)
	}
	if outcome != OutcomeUnsettled {
		t.Fatalf("expected OutcomeUnsettled, got %q", outcome)
	}
}

func TestGetSettlementBySlugReturnsOutcomeUnsettledForNonExactWinningArrays(t *testing.T) {
	tests := []struct {
		name          string
		outcomePrices string
	}{
		{name: "both_sides_one", outcomePrices: `["1","1"]`},
		{name: "single_side_only", outcomePrices: `["1"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, fmt.Sprintf(`[{"markets":[{"closed":true,"outcomePrices":%q}]}]`, tt.outcomePrices))
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

			outcome, err := client.GetSettlementBySlug(context.Background(), "non-exact")
			if err != nil {
				t.Fatalf("GetSettlementBySlug() error: %v", err)
			}
			if outcome != OutcomeUnsettled {
				t.Fatalf("expected OutcomeUnsettled, got %q", outcome)
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
	if typed.Op != "gamma.new" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestGetSettlementBySlugReturnsTypedDecodeErrorForMalformedBody(t *testing.T) {
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

	_, err = client.GetSettlementBySlug(context.Background(), "bad-body")
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
	if string(typed.RawBody) != "{" {
		t.Fatalf("unexpected raw body: %q", string(typed.RawBody))
	}
}

func TestGetSettlementBySlugReturnsTypedDecodeErrorForMalformedOutcomePrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"markets":[{"closed":true,"outcomePrices":"not-json"}]}]`)
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

	_, err = client.GetSettlementBySlug(context.Background(), "bad-outcomes")
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
	if typed.Op != "gamma.settlement_by_slug" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}
