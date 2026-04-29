package balances

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"
	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

type Client struct {
	authClient *polyauth.Client
	signer     *polyauth.Signer
	transport  *polyauth.Transport
}

const defaultRequestSignatureType = 2

func NewClient(httpClient *httpx.Client, authConfig polyauth.Config) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "balances.new",
			Message: "http transport is required",
		}
	}
	if err := authConfig.Validate(); err != nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrAuth,
			Op:      "balances.new",
			Message: err.Error(),
			Cause:   err,
		}
	}

	authClient, err := polyauth.NewClient(httpClient, authConfig)
	if err != nil {
		return nil, err
	}
	signer, err := polyauth.NewSigner(authConfig)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "balances.new", Message: err.Error(), Cause: err}
	}
	transport, err := polyauth.NewTransport(httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{
		authClient: authClient,
		signer:     signer,
		transport:  transport,
	}, nil
}

func (c *Client) GetBalance(ctx context.Context, req GetBalanceRequest) (GetBalanceResponse, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return GetBalanceResponse{}, err
	}

	assetType := req.AssetType
	if assetType == "" {
		assetType = AssetTypeCollateral
	}
	signatureType := effectiveSignatureType(req.SignatureType)

	query := url.Values{}
	query.Set("asset_type", string(assetType))
	query.Set("signature_type", strconv.Itoa(signatureType))
	if s := strings.TrimSpace(req.TokenID); s != "" {
		query.Set("token_id", s)
	}

	headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
		Method:      http.MethodGet,
		RequestPath: "/balance-allowance",
	}, polyauth.Now())
	if err != nil {
		return GetBalanceResponse{}, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "balances.get_balance", Message: err.Error(), Cause: err}
	}

	body, err := c.transport.DoJSON(ctx, polyauth.TransportRequest{
		Op:      "balances.get_balance",
		Method:  http.MethodGet,
		Path:    "/balance-allowance",
		Query:   query,
		Headers: headers.HTTPHeader(),
	})
	if err != nil {
		return GetBalanceResponse{}, err
	}

	var resp GetBalanceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return GetBalanceResponse{}, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "balances.get_balance", Message: err.Error(), Cause: err, RawBody: body}
	}
	return resp, nil
}

func (c *Client) UpdateAllowance(ctx context.Context, req UpdateAllowanceRequest) (UpdateAllowanceResponse, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return UpdateAllowanceResponse{}, err
	}

	assetType := req.AssetType
	if assetType == "" {
		assetType = AssetTypeCollateral
	}
	signatureType := effectiveSignatureType(req.SignatureType)

	query := url.Values{}
	query.Set("asset_type", assetType)
	query.Set("signature_type", strconv.Itoa(signatureType))
	if tokenID := strings.TrimSpace(req.TokenID); tokenID != "" {
		query.Set("token_id", tokenID)
	}

	headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
		Method:      http.MethodGet,
		RequestPath: "/balance-allowance/update",
	}, polyauth.Now())
	if err != nil {
		return UpdateAllowanceResponse{}, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "balances.update_allowance", Message: err.Error(), Cause: err}
	}

	body, err := c.transport.DoJSON(ctx, polyauth.TransportRequest{
		Op:      "balances.update_allowance",
		Method:  http.MethodGet,
		Path:    "/balance-allowance/update",
		Query:   query,
		Headers: headers.HTTPHeader(),
	})
	if err != nil {
		return UpdateAllowanceResponse{}, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return UpdateAllowanceResponse{}, nil
	}

	var resp UpdateAllowanceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return UpdateAllowanceResponse{}, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "balances.update_allowance", Message: err.Error(), Cause: err, RawBody: body}
	}
	return resp, nil
}

func (c *Client) credentials(ctx context.Context, creds polyauth.APICredentials) (polyauth.APICredentials, error) {
	if creds.Valid() {
		return creds, nil
	}
	derived, err := c.authClient.EnsureCredentials(ctx)
	if err != nil {
		return polyauth.APICredentials{}, err
	}
	return derived.APICredentials, nil
}

func effectiveSignatureType(signatureType int) int {
	if signatureType <= 0 {
		return defaultRequestSignatureType
	}
	return signatureType
}
