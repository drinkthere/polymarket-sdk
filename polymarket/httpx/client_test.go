package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDoJSONSuccessReturnsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	got, err := client.DoJSON(context.Background(), Request{
		Op:     "markets.list",
		Method: http.MethodGet,
		Path:   "/",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("unexpected payload: %q", string(got))
	}
}

func TestDoJSONReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"bad_request","message":"nope"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	_, err = client.DoJSON(context.Background(), Request{
		Op:     "markets.list",
		Method: http.MethodGet,
		Path:   "/",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrAPI {
		t.Fatalf("expected ErrAPI, got %v", typed.Kind)
	}
	if typed.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", typed.StatusCode)
	}
}

func TestNewInvalidBaseURLReturnsRequestBuildError(t *testing.T) {
	_, err := New(ClientConfig{
		BaseURL:  "http://[::1",
		Timeout: time.Second,
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
	var ue *url.Error
	if !errors.As(err, &ue) {
		t.Fatalf("expected errors.As to match *url.Error via Unwrap, got %T", err)
	}
}

func TestDoJSONJSONEncodingFailureReturnsRequestBuildError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	type badBody struct {
		Ch chan int `json:"ch"`
	}

	_, err = client.DoJSON(context.Background(), Request{
		Op:     "markets.list",
		Method: http.MethodPost,
		Path:   "/",
		Body:   badBody{Ch: make(chan int)},
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
	var ute *json.UnsupportedTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("expected errors.As to match *json.UnsupportedTypeError via Unwrap, got %T", err)
	}
}

func TestDoJSONTimeoutAndCancelClassifyAsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	client.httpClient.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.DoJSON(canceledCtx, Request{Op: "t.cancel", Method: http.MethodGet, Path: "/"})
	if err == nil {
		t.Fatal("expected error")
	}
	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrTimeout {
		t.Fatalf("expected ErrTimeout for canceled request, got %v", typed.Kind)
	}

	timeoutCtx, timeoutCancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	timeoutCancel()
	_, err = client.DoJSON(timeoutCtx, Request{Op: "t.timeout", Method: http.MethodGet, Path: "/"})
	if err == nil {
		t.Fatal("expected error")
	}
	typed = nil
	if !errors.As(err, &typed) {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrTimeout {
		t.Fatalf("expected ErrTimeout for timed-out request, got %v", typed.Kind)
	}
}

func TestDoJSONEnforcesMaxResponseBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "0123456789abcdef")
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second, MaxResponseBytes: 8})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = client.DoJSON(context.Background(), Request{
		Op:     "markets.list",
		Method: http.MethodGet,
		Path:   "/",
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
	if len(typed.RawBody) != 8 {
		t.Fatalf("expected RawBody length 8, got %d", len(typed.RawBody))
	}
}

