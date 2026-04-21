package markets_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		if got := r.URL.Query().Get("variant"); got != " fiveminute " {
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

	got, err := client.GetCryptoPriceOpen(context.Background(), markets.CryptoPriceRequest{
		Symbol:         " btc ",
		Variant:        " fiveminute ",
		EventStartTime: time.Date(2026, 4, 21, 11, 25, 0, 0, time.UTC),
		EndDate:        time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GetCryptoPriceOpen: %v", err)
	}
	if got.OpenPrice != 84250.5 {
		t.Fatalf("unexpected open price: %.4f", got.OpenPrice)
	}
}
