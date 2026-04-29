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
	assetType, tokenID, signatureType, err := normalizeRequest("balances.get_balance", req.AssetType, req.TokenID, req.SignatureType)
	if err != nil {
		return GetBalanceResponse{}, err
	}

	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return GetBalanceResponse{}, err
	}

	query := url.Values{}
	query.Set("asset_type", string(assetType))
	query.Set("signature_type", strconv.Itoa(signatureType))
	if tokenID != "" {
		query.Set("token_id", tokenID)
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
	assetType, tokenID, signatureType, err := normalizeRequest("balances.update_allowance", req.AssetType, req.TokenID, req.SignatureType)
	if err != nil {
		return UpdateAllowanceResponse{}, err
	}

	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return UpdateAllowanceResponse{}, err
	}

	query := url.Values{}
	query.Set("asset_type", assetType)
	query.Set("signature_type", strconv.Itoa(signatureType))
	if tokenID != "" {
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
		return UpdateAllowanceResponse{Updated: true}, nil
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

func normalizeRequest(op, assetType, tokenID string, signatureType int) (string, string, int, error) {
	normalizedAssetType, err := normalizeAssetType(op, assetType)
	if err != nil {
		return "", "", 0, err
	}

	trimmed := strings.TrimSpace(tokenID)
	if normalizedAssetType == AssetTypeConditional && trimmed == "" {
		return "", "", 0, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "token_id is required for conditional asset type",
		}
	}
	return normalizedAssetType, trimmed, effectiveSignatureType(signatureType), nil
}

func normalizeAssetType(op, assetType string) (string, error) {
	trimmed := strings.TrimSpace(assetType)
	if trimmed == "" {
		return AssetTypeCollateral, nil
	}
	switch trimmed {
	case AssetTypeCollateral, AssetTypeConditional:
		return trimmed, nil
	default:
		return "", &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      op,
			Message: "asset_type must be COLLATERAL or CONDITIONAL",
		}
	}
}
