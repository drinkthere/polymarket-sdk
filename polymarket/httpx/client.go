package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

type ClientConfig struct {
	BaseURL string
	Timeout time.Duration
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Request struct {
	Op     string
	Method string
	Path   string
	Body   any
}

func New(cfg ClientConfig) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (c *Client) DoJSON(ctx context.Context, req Request) ([]byte, error) {
	var body io.Reader
	if req.Body != nil {
		buf := bytes.NewBuffer(nil)
		if err := json.NewEncoder(buf).Encode(req.Body); err != nil {
			return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
		}
		body = buf
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, c.baseURL+req.Path, body)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
	}
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:   polyerrors.ErrNetwork,
			Op:     req.Op,
			Method: req.Method,
			URL:    httpReq.URL.String(),
			Message: err.Error(),
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	payload, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrDecode,
			Op:         req.Op,
			Method:     req.Method,
			URL:        httpReq.URL.String(),
			StatusCode: resp.StatusCode,
			Message:    readErr.Error(),
			Cause:      readErr,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrAPI,
			Op:         req.Op,
			Method:     req.Method,
			URL:        httpReq.URL.String(),
			StatusCode: resp.StatusCode,
			Message:    string(payload),
			RawBody:    payload,
		}
	}

	return payload, nil
}

