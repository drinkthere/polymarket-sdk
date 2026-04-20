package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

type ClientConfig struct {
	BaseURL          string
	Timeout          time.Duration
	MaxResponseBytes int64
}

type Client struct {
	baseURL          *url.URL
	httpClient       *http.Client
	maxResponseBytes int64
}

type Request struct {
	Op     string
	Method string
	Path   string
	Body   any
}

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = int64(2 << 20) // 2 MiB
)

func New(cfg ClientConfig) (*Client, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "httpx.new",
			Message: "base_url is required",
		}
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "httpx.new", Message: err.Error(), Cause: err}
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "httpx.new",
			Message: "base_url must be an absolute URL",
		}
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "httpx.new",
			Message: "base_url scheme must be http or https",
		}
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	maxResp := cfg.MaxResponseBytes
	if maxResp == 0 {
		maxResp = defaultMaxResponseBytes
	}
	if maxResp < 0 {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "httpx.new",
			Message: "max_response_bytes must be >= 0",
		}
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")

	return &Client{
		baseURL:          parsed,
		maxResponseBytes: maxResp,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *Client) buildURL(p string) string {
	u := *c.baseURL
	u.Path = path.Join(c.baseURL.Path, p)
	return u.String()
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (c *Client) readAllLimited(r io.Reader) (payload []byte, truncated bool, err error) {
	limit := c.maxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}

	lr := io.LimitReader(r, limit+1)
	payload, err = io.ReadAll(lr)
	if err != nil {
		return nil, false, err
	}
	if int64(len(payload)) <= limit {
		return payload, false, nil
	}
	return payload[:limit], true, nil
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

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, c.buildURL(req.Path), body)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
	}
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		kind := polyerrors.ErrNetwork
		if isTimeoutErr(err) {
			kind = polyerrors.ErrTimeout
		}
		return nil, &polyerrors.Error{
			Kind:   kind,
			Op:     req.Op,
			Method: req.Method,
			URL:    httpReq.URL.String(),
			Message: err.Error(),
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	payload, truncated, readErr := c.readAllLimited(resp.Body)
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

	if truncated && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrProtocol,
			Op:         req.Op,
			Method:     req.Method,
			URL:        httpReq.URL.String(),
			StatusCode: resp.StatusCode,
			Message:    "response body exceeds max_response_bytes=" + strconv.FormatInt(c.maxResponseBytes, 10),
			RawBody:    payload,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(payload)
		if truncated {
			msg = msg + " (truncated)"
		}
		return nil, &polyerrors.Error{
			Kind:       polyerrors.ErrAPI,
			Op:         req.Op,
			Method:     req.Method,
			URL:        httpReq.URL.String(),
			StatusCode: resp.StatusCode,
			Message:    msg,
			RawBody:    payload,
		}
	}

	return payload, nil
}
