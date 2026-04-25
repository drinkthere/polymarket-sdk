package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

type Transport struct {
	baseURL          *url.URL
	httpClient       *http.Client
	maxResponseBytes int64
}

type TransportRequest struct {
	Op      string
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

func NewTransport(client *httpx.Client) (*Transport, error) {
	if client == nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "auth.transport", Message: "http transport is required"}
	}

	rv := reflect.ValueOf(client)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "auth.transport", Message: "http transport is invalid"}
	}
	elem := rv.Elem()

	baseURL, ok := readUnexported[*url.URL](elem, "baseURL")
	if !ok || baseURL == nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "auth.transport", Message: "http transport missing baseURL"}
	}

	httpClient, ok := readUnexported[*http.Client](elem, "httpClient")
	if !ok || httpClient == nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "auth.transport", Message: "http transport missing httpClient"}
	}

	maxResponseBytes, ok := readUnexported[int64](elem, "maxResponseBytes")
	if !ok || maxResponseBytes == 0 {
		maxResponseBytes = 2 << 20
	}

	return &Transport{
		baseURL:          baseURL,
		httpClient:       httpClient,
		maxResponseBytes: maxResponseBytes,
	}, nil
}

func readUnexported[T any](elem reflect.Value, fieldName string) (T, bool) {
	var zero T
	field := elem.FieldByName(fieldName)
	if !field.IsValid() {
		return zero, false
	}
	ptr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr()))
	value, ok := ptr.Elem().Interface().(T)
	return value, ok
}

func (t *Transport) DoJSON(ctx context.Context, req TransportRequest) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	reqURL, err := t.buildURL(req.Path, req.Query)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, reqURL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
	}
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, values := range req.Headers {
		for _, v := range values {
			httpReq.Header.Add(k, v)
		}
	}

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		kind := polyerrors.ErrNetwork
		if errors.Is(err, context.Canceled) {
			kind = polyerrors.ErrClosed
		} else if errors.Is(err, context.DeadlineExceeded) || isTimeoutErr(err) {
			kind = polyerrors.ErrTimeout
		}
		return nil, &polyerrors.Error{
			Kind:    kind,
			Op:      req.Op,
			Method:  req.Method,
			URL:     reqURL,
			Message: err.Error(),
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	payload, truncated, readErr := t.readAllLimited(resp.Body)
	if readErr != nil {
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrDecode,
			Op:         req.Op,
			Method:     req.Method,
			URL:        reqURL,
			StatusCode: resp.StatusCode,
			Message:    readErr.Error(),
			Cause:      readErr,
		}
	}

	if truncated && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrProtocol,
			Op:         req.Op,
			Method:     req.Method,
			URL:        reqURL,
			StatusCode: resp.StatusCode,
			Message:    "response body exceeds max_response_bytes=" + strconv.FormatInt(t.maxResponseBytes, 10),
			RawBody:    payload,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(payload)
		if truncated {
			msg += " (truncated)"
		}
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrAPI,
			Op:         req.Op,
			Method:     req.Method,
			URL:        reqURL,
			StatusCode: resp.StatusCode,
			Message:    msg,
			RawBody:    payload,
		}
	}

	return payload, nil
}

func (t *Transport) buildURL(path string, query url.Values) (string, error) {
	if strings.ContainsAny(path, "?#") {
		return "", errors.New("path must not contain '?' or '#'")
	}
	u := t.baseURL.JoinPath(path)
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func (t *Transport) readAllLimited(r io.Reader) ([]byte, bool, error) {
	limit := t.maxResponseBytes
	if limit <= 0 {
		limit = 2 << 20
	}

	readLimit := limit
	if limit < math.MaxInt64 {
		readLimit = limit + 1
	}

	payload, err := io.ReadAll(io.LimitReader(r, readLimit))
	if err != nil {
		return nil, false, err
	}
	if int64(len(payload)) <= limit {
		return payload, false, nil
	}
	return payload[:limit], true, nil
}

func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
