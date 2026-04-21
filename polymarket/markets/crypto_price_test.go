package markets_test

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
	"github.com/drinkthere/polymarket-sdk/polymarket/markets"
)

func TestGetCryptoPriceOpenBuildsExpectedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/crypto/crypto-price" {
			t.Fatalf("unexpected path: %s", got)
		}
		if got := r.URL.Query().Get("symbol"); got != "BTC" {
			t.Fatalf("unexpected symbol: %s", got)
		}
		if got := r.URL.Query().Get("variant"); got != "fiveminute" {
			t.Fatalf("unexpected variant: %s", got)
		}
		if got := r.URL.Query().Get("eventStartTime"); got != "2026-04-21T11:25:00Z" {
			t.Fatalf("unexpected eventStartTime: %s", got)
		}
		if got := r.URL.Query().Get("endDate"); got != "2026-04-21T11:30:00Z" {
			t.Fatalf("unexpected endDate: %s", got)
		}
		_, _ = io.WriteString(w, `{"openPrice":84250.5}`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := markets.NewClient(httpClient)
	if err != nil {
		t.Fatalf("markets.NewClient: %v", err)
	}

	berlinSummer := time.FixedZone("UTC+2", 2*60*60)
	got, err := client.GetCryptoPriceOpen(context.Background(), markets.CryptoPriceRequest{
		Symbol:         " btc ",
		Variant:        " fiveminute ",
		EventStartTime: time.Date(2026, 4, 21, 13, 25, 0, 0, berlinSummer),
		EndDate:        time.Date(2026, 4, 21, 13, 30, 0, 0, berlinSummer),
	})
	if err != nil {
		t.Fatalf("GetCryptoPriceOpen: %v", err)
	}
	if got.OpenPrice != 84250.5 {
		t.Fatalf("unexpected open price: %.4f", got.OpenPrice)
	}
}

func TestGetCryptoPriceOpenReturnsTypedDecodeErrorForMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := markets.NewClient(httpClient)
	if err != nil {
		t.Fatalf("markets.NewClient: %v", err)
	}

	_, err = client.GetCryptoPriceOpen(context.Background(), markets.CryptoPriceRequest{
		Symbol:         "BTC",
		Variant:        "fiveminute",
		EventStartTime: time.Date(2026, 4, 21, 11, 25, 0, 0, time.UTC),
		EndDate:        time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC),
	})
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

func TestGetCryptoPriceOpenReturnsTypedProtocolErrorForNonPositiveOpenPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"openPrice":0}`)
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := markets.NewClient(httpClient)
	if err != nil {
		t.Fatalf("markets.NewClient: %v", err)
	}

	_, err = client.GetCryptoPriceOpen(context.Background(), markets.CryptoPriceRequest{
		Symbol:         "BTC",
		Variant:        "fiveminute",
		EventStartTime: time.Date(2026, 4, 21, 11, 25, 0, 0, time.UTC),
		EndDate:        time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC),
	})
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

func TestGetCryptoPriceOpenReturnsTypedRequestBuildErrorForInvalidInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unexpected request")
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := markets.NewClient(httpClient)
	if err != nil {
		t.Fatalf("markets.NewClient: %v", err)
	}

	testCases := []struct {
		name string
		req  markets.CryptoPriceRequest
	}{
		{
			name: "empty symbol",
			req: markets.CryptoPriceRequest{
				Symbol:         "   ",
				Variant:        "fiveminute",
				EventStartTime: time.Date(2026, 4, 21, 11, 25, 0, 0, time.UTC),
				EndDate:        time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "empty variant",
			req: markets.CryptoPriceRequest{
				Symbol:         "BTC",
				Variant:        "   ",
				EventStartTime: time.Date(2026, 4, 21, 11, 25, 0, 0, time.UTC),
				EndDate:        time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "zero event start time",
			req: markets.CryptoPriceRequest{
				Symbol:  "BTC",
				Variant: "fiveminute",
				EndDate: time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "zero end date",
			req: markets.CryptoPriceRequest{
				Symbol:         "BTC",
				Variant:        "fiveminute",
				EventStartTime: time.Date(2026, 4, 21, 11, 25, 0, 0, time.UTC),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.GetCryptoPriceOpen(context.Background(), tc.req)
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
			if typed.Op != "crypto_price.get_open" {
				t.Fatalf("unexpected op: %q", typed.Op)
			}
		})
	}
}
