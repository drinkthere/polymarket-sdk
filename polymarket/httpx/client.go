package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
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
	Query  url.Values
	Body   any
	Header http.Header
}

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = int64(2 << 20) // 2 MiB
)

func New(cfg ClientConfig) (*Client, error) {
	parsed, maxResp, err := validateConfigWithOp(cfg, "httpx.new")
	if err != nil {
		return nil, err
	}

	timeout := effectiveTimeout(cfg.Timeout)

	return &Client{
		baseURL:          parsed,
		maxResponseBytes: maxResp,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func NewWithHTTPClient(cfg ClientConfig, raw *http.Client) (*Client, error) {
	if raw == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "httpx.new_with_http_client",
			Message: "http client is required",
		}
	}

	base, maxResp, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}

	cloned := *raw
	cloned.Timeout = effectiveTimeout(cfg.Timeout)

	return &Client{
		baseURL:          base,
		httpClient:       &cloned,
		maxResponseBytes: maxResp,
	}, nil
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultTimeout
	}
	return timeout
}

func validateConfig(cfg ClientConfig) (*url.URL, int64, error) {
	return validateConfigWithOp(cfg, "httpx.new_with_http_client")
}

func validateConfigWithOp(cfg ClientConfig, op string) (*url.URL, int64, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, 0, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "base_url is required",
		}
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, 0, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: op, Message: err.Error(), Cause: err}
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return nil, 0, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "base_url must be an absolute URL",
		}
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, 0, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "base_url scheme must be http or https",
		}
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, 0, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "base_url must not contain query or fragment",
		}
	}

	maxResp := cfg.MaxResponseBytes
	if maxResp == 0 {
		maxResp = defaultMaxResponseBytes
	}
	if maxResp < 0 {
		return nil, 0, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "max_response_bytes must be >= 0",
		}
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, maxResp, nil
}

func (c *Client) buildURL(p string, query url.Values) (string, error) {
	if strings.ContainsAny(p, "?#") {
		return "", errors.New("path must not contain '?' or '#'")
	}
	u := c.baseURL.JoinPath(p)
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func isNetTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (c *Client) readAllLimited(r io.Reader) (payload []byte, truncated bool, err error) {
	limit := c.maxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}

	readLimit := limit
	if limit < math.MaxInt64 {
		readLimit = limit + 1
	}
	lr := io.LimitReader(r, readLimit)
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
	if ctx == nil {
		ctx = context.Background()
	}

	var body io.Reader
	if req.Body != nil {
		buf := bytes.NewBuffer(nil)
		if err := json.NewEncoder(buf).Encode(req.Body); err != nil {
			return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
		}
		body = buf
	}

	reqURL, err := c.buildURL(req.Path, req.Query)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, reqURL, body)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: req.Op, Message: err.Error(), Cause: err}
	}
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for key, values := range req.Header {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		kind := polyerrors.ErrNetwork
		if errors.Is(err, context.Canceled) {
			kind = polyerrors.ErrClosed
		} else if errors.Is(err, context.DeadlineExceeded) || isNetTimeoutErr(err) {
			kind = polyerrors.ErrTimeout
		}
		return nil, &polyerrors.Error{
			Kind:    kind,
			Op:      req.Op,
			Method:  req.Method,
			URL:     httpReq.URL.String(),
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
