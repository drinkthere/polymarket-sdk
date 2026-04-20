package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

func TestDoJSONReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"bad_request","message":"nope"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	_, err := client.DoJSON(context.Background(), Request{
		Op:     "markets.list",
		Method: http.MethodGet,
		Path:   "/",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	typed, ok := err.(*polyerrors.Error)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrAPI {
		t.Fatalf("expected ErrAPI, got %v", typed.Kind)
	}
	if typed.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", typed.StatusCode)
	}
}

