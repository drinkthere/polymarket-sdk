package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestNewWithHTTPClientUsesInjectedClient(t *testing.T) {
	used := false
	rawClient := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			used = true
			if r.Header.Get("User-Agent") != "pm5-test" {
				t.Fatalf("ua = %q", r.Header.Get("User-Agent"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}
	client, err := NewWithHTTPClient(ClientConfig{BaseURL: "https://example.com"}, rawClient)
	if err != nil {
		t.Fatalf("NewWithHTTPClient() error = %v", err)
	}

	body, err := client.DoJSON(t.Context(), Request{
		Op:     "httpx.injected_client",
		Method: http.MethodGet,
		Path:   "/",
		Header: http.Header{"User-Agent": []string{"pm5-test"}},
	})
	if err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("DoJSON() body = %s", string(body))
	}
	if !used {
		t.Fatal("expected injected client to be used")
	}
}

func TestNewWithHTTPClientAppliesConfiguredTimeoutToClonedClient(t *testing.T) {
	rawClient := &http.Client{
		Timeout:   7 * time.Second,
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) { return nil, nil }),
	}

	client, err := NewWithHTTPClient(ClientConfig{
		BaseURL: "https://example.com",
		Timeout: 1500 * time.Millisecond,
	}, rawClient)
	if err != nil {
		t.Fatalf("NewWithHTTPClient() error = %v", err)
	}

	if client.httpClient == rawClient {
		t.Fatal("expected NewWithHTTPClient to clone the injected client")
	}
	if client.httpClient.Transport == nil {
		t.Fatal("expected cloned client to keep an injected transport")
	}
	if client.httpClient.Timeout != 1500*time.Millisecond {
		t.Fatalf("client timeout = %s, want %s", client.httpClient.Timeout, 1500*time.Millisecond)
	}
	if rawClient.Timeout != 7*time.Second {
		t.Fatalf("raw client timeout mutated to %s", rawClient.Timeout)
	}
}

func TestNewWithHTTPClientPreservesInjectedTimeoutWhenUnset(t *testing.T) {
	rawClient := &http.Client{
		Timeout:   0,
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) { return nil, nil }),
	}

	client, err := NewWithHTTPClient(ClientConfig{
		BaseURL: "https://example.com",
	}, rawClient)
	if err != nil {
		t.Fatalf("NewWithHTTPClient() error = %v", err)
	}

	if client.httpClient == rawClient {
		t.Fatal("expected NewWithHTTPClient to clone the injected client")
	}
	if client.httpClient.Timeout != 0 {
		t.Fatalf("client timeout = %s, want 0s", client.httpClient.Timeout)
	}
	if rawClient.Timeout != 0 {
		t.Fatalf("raw client timeout mutated to %s", rawClient.Timeout)
	}
}

func TestNewWithHTTPClientRejectsNilClient(t *testing.T) {
	_, err := NewWithHTTPClient(ClientConfig{BaseURL: "https://example.com"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var perr *polyerrors.Error
	if !errors.As(err, &perr) {
		t.Fatalf("expected *polyerrors.Error, got %T", err)
	}
	if perr.Kind != polyerrors.ErrRequestBuild {
		t.Fatalf("kind = %v", perr.Kind)
	}
}

func TestNewInvalidBaseURLReturnsRequestBuildError(t *testing.T) {
	_, err := New(ClientConfig{
		BaseURL: "http://[::1",
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

func TestDoJSONTimeoutAndCancelClassifyCorrectly(t *testing.T) {
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
	if typed.Kind != polyerrors.ErrClosed {
		t.Fatalf("expected ErrClosed for canceled request, got %v", typed.Kind)
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

func TestReadAllLimitedDoesNotOverflowMaxInt64(t *testing.T) {
	client, err := New(ClientConfig{BaseURL: "https://example.com", Timeout: time.Second, MaxResponseBytes: int64(^uint64(0) >> 1)})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	payload, truncated, err := client.readAllLimited(bytes.NewBufferString("hello"))
	if err != nil {
		t.Fatalf("readAllLimited() error: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated=false")
	}
	if string(payload) != "hello" {
		t.Fatalf("unexpected payload: %q", string(payload))
	}
}

func TestDoJSONNilContextDefaultsToBackground(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	got, err := client.DoJSON(nil, Request{
		Op:     "t.nilctx",
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

func TestDoJSONBuildURLJoinsBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/markets" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected query: %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL + "/api", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = client.DoJSON(context.Background(), Request{
		Op:     "t.joinpath",
		Method: http.MethodGet,
		Path:   "/v1/markets",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestDoJSONRejectsPathWithQueryOrFragment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for request build errors")
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "query", path: "/v1/markets?foo=bar"},
		{name: "fragment", path: "/v1/markets#frag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.DoJSON(context.Background(), Request{
				Op:     "t.badpath",
				Method: http.MethodGet,
				Path:   tc.path,
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
		})
	}
}

func TestNewRejectsBaseURLWithQueryOrFragment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}))
	defer server.Close()

	for _, tc := range []struct {
		name    string
		baseURL string
	}{
		{name: "query", baseURL: server.URL + "/api?foo=bar"},
		{name: "fragment", baseURL: server.URL + "/api#frag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(ClientConfig{BaseURL: tc.baseURL, Timeout: time.Second})
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
		})
	}
}

func TestNewWithHTTPClientRejectsBaseURLWithInvalidSchemeOrQueryFragment(t *testing.T) {
	rawClient := &http.Client{}

	for _, tc := range []struct {
		name    string
		baseURL string
	}{
		{name: "invalid_scheme", baseURL: "ftp://example.com/api"},
		{name: "query", baseURL: "https://example.com/api?foo=bar"},
		{name: "fragment", baseURL: "https://example.com/api#frag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWithHTTPClient(ClientConfig{BaseURL: tc.baseURL}, rawClient)
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
		})
	}
}
