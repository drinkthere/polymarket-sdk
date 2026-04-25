package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

type Client struct {
	transport *Transport
	cfg       Config
	signer    *Signer
}

func NewClient(httpClient *httpx.Client, cfg Config) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "auth.new", Message: "http transport is required"}
	}
	if err := cfg.Validate(); err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "auth.new", Message: err.Error(), Cause: err}
	}

	signer, err := NewSigner(cfg)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "auth.new", Message: err.Error(), Cause: err}
	}
	transport, err := NewTransport(httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{
		transport: transport,
		cfg:       cfg,
		signer:    signer,
	}, nil
}

func (c *Client) EnsureCredentials(ctx context.Context) (Credentials, error) {
	if creds := c.signer.CredentialsFromConfig(c.cfg); creds.Valid() {
		return creds, nil
	}
	return c.CreateOrDeriveAPIKey(ctx, 0)
}

func (c *Client) CreateAPIKey(ctx context.Context, nonce int64) (Credentials, error) {
	return c.requestCredentials(ctx, "auth.create_api_key", http.MethodPost, "/auth/api-key", nonce)
}

func (c *Client) DeriveAPIKey(ctx context.Context, nonce int64) (Credentials, error) {
	return c.requestCredentials(ctx, "auth.derive_api_key", http.MethodGet, "/auth/derive-api-key", nonce)
}

func (c *Client) CreateOrDeriveAPIKey(ctx context.Context, nonce int64) (Credentials, error) {
	creds, err := c.CreateAPIKey(ctx, nonce)
	if err == nil && creds.Valid() {
		return creds, nil
	}

	if err != nil {
		var typed *polyerrors.Error
		if !errors.As(err, &typed) || typed.Kind != polyerrors.ErrAPI || typed.StatusCode != http.StatusConflict {
			return Credentials{}, err
		}
	}

	return c.DeriveAPIKey(ctx, nonce)
}

func (c *Client) requestCredentials(ctx context.Context, op, method, path string, nonce int64) (Credentials, error) {
	headers, err := c.signer.CreateL1Headers(time.Now(), nonce)
	if err != nil {
		return Credentials{}, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: op, Message: err.Error(), Cause: err}
	}

	body, err := c.transport.DoJSON(ctx, TransportRequest{
		Op:      op,
		Method:  method,
		Path:    path,
		Headers: headers.HTTPHeader(),
	})
	if err != nil {
		return Credentials{}, err
	}

	var creds Credentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return Credentials{}, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: op, Message: err.Error(), Cause: err, RawBody: body}
	}
	creds.Address = c.signer.Address()
	creds.FunderAddress = strings.ToLower(c.cfg.FunderAddress)
	return creds, nil
}

func (h L1PolyHeader) HTTPHeader() http.Header {
	header := http.Header{}
	header.Set("POLY_ADDRESS", h.POLY_ADDRESS)
	header.Set("POLY_SIGNATURE", h.POLY_SIGNATURE)
	header.Set("POLY_TIMESTAMP", h.POLY_TIMESTAMP)
	header.Set("POLY_NONCE", h.POLY_NONCE)
	return header
}

func (h L2PolyHeader) HTTPHeader() http.Header {
	header := http.Header{}
	header.Set("POLY_ADDRESS", h.POLY_ADDRESS)
	header.Set("POLY_SIGNATURE", h.POLY_SIGNATURE)
	header.Set("POLY_TIMESTAMP", h.POLY_TIMESTAMP)
	header.Set("POLY_API_KEY", h.POLY_API_KEY)
	header.Set("POLY_PASSPHRASE", h.POLY_PASSPHRASE)
	return header
}

func Now() time.Time { return time.Now() }
