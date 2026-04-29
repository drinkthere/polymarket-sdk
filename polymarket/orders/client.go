package orders

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"
	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

const endCursor = "LTE="

type Client struct {
	authClient *polyauth.Client
	signer     *polyauth.Signer
	transport  *polyauth.Transport
}

func NewClient(httpClient *httpx.Client, authConfig polyauth.Config) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "orders.new", Message: "http transport is required"}
	}
	if err := authConfig.Validate(); err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "orders.new", Message: err.Error(), Cause: err}
	}

	authClient, err := polyauth.NewClient(httpClient, authConfig)
	if err != nil {
		return nil, err
	}
	signer, err := polyauth.NewSigner(authConfig)
	if err != nil {
		return nil, &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: "orders.new", Message: err.Error(), Cause: err}
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

func (c *Client) CreateOrDeriveAPIKey(ctx context.Context, nonce int64) (polyauth.APICredentials, error) {
	creds, err := c.authClient.CreateOrDeriveAPIKey(ctx, nonce)
	if err != nil {
		return polyauth.APICredentials{}, err
	}
	return creds.APICredentials, nil
}

func (c *Client) GetOpenOrders(ctx context.Context, req GetOpenOrdersRequest) ([]OpenOrder, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return nil, err
	}

	baseQuery := url.Values{}
	if s := strings.TrimSpace(req.Market); s != "" {
		baseQuery.Set("market", s)
	}
	if s := strings.TrimSpace(req.AssetID); s != "" {
		baseQuery.Set("asset_id", s)
	}

	var all []OpenOrder
	nextCursor := "MA=="
	for {
		query := cloneQuery(baseQuery)
		query.Set("next_cursor", nextCursor)

		headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
			Method:      http.MethodGet,
			RequestPath: "/data/orders",
		}, polyauth.Now())
		if err != nil {
			return nil, authError("orders.get_open_orders", err)
		}

		body, err := c.transport.DoJSON(ctx, polyauth.TransportRequest{
			Op:      "orders.get_open_orders",
			Method:  http.MethodGet,
			Path:    "/data/orders",
			Query:   query,
			Headers: headers.HTTPHeader(),
		})
		if err != nil {
			return nil, err
		}

		var page struct {
			Data       []OpenOrder `json:"data"`
			NextCursor string      `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, decodeError("orders.get_open_orders", body, err)
		}

		all = append(all, page.Data...)
		if strings.TrimSpace(page.NextCursor) == "" || page.NextCursor == endCursor {
			return all, nil
		}
		nextCursor = page.NextCursor
	}
}

func (c *Client) GetUserTrades(ctx context.Context, req GetUserTradesRequest) ([]UserTrade, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return nil, err
	}

	baseQuery := userTradesBaseQuery(req.ID, req.MakerAddress, req.Market, req.AssetID, req.Before, req.After)

	var all []UserTrade
	nextCursor := "MA=="
	for {
		body, err := c.getUserTradesPage(ctx, creds, nextCursor, baseQuery, "orders.get_user_trades")
		if err != nil {
			return nil, err
		}

		var page struct {
			Data       []UserTrade `json:"data"`
			NextCursor string      `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, decodeError("orders.get_user_trades", body, err)
		}

		all = append(all, page.Data...)
		if strings.TrimSpace(page.NextCursor) == "" || page.NextCursor == endCursor {
			return all, nil
		}
		nextCursor = page.NextCursor
	}
}

func (c *Client) GetUserTradesRaw(ctx context.Context, req GetUserTradesRawRequest) ([]json.RawMessage, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return nil, err
	}

	baseQuery := userTradesBaseQuery(req.ID, req.MakerAddress, req.Market, req.AssetID, req.Before, req.After)

	var all []json.RawMessage
	nextCursor := "MA=="
	for {
		body, err := c.getUserTradesPage(ctx, creds, nextCursor, baseQuery, "orders.get_user_trades_raw")
		if err != nil {
			return nil, err
		}

		var page struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor string            `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, decodeError("orders.get_user_trades_raw", body, err)
		}

		all = append(all, page.Data...)
		if strings.TrimSpace(page.NextCursor) == "" || page.NextCursor == endCursor {
			return all, nil
		}
		nextCursor = page.NextCursor
	}
}

func (c *Client) PlaceMakerOrder(ctx context.Context, req PlaceMakerOrderRequest) (PlaceMakerOrderResponse, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return PlaceMakerOrderResponse{}, err
	}

	orderType := req.OrderType
	if orderType == "" {
		orderType = OrderTypeGTC
	}

	owner := strings.TrimSpace(req.Owner)
	if owner == "" {
		owner = creds.Key
	}
	payload := struct {
		Order     MakerOrder `json:"order"`
		Owner     string     `json:"owner"`
		OrderType OrderType  `json:"orderType"`
		DeferExec bool       `json:"deferExec"`
		PostOnly  bool       `json:"postOnly"`
	}{
		Order:     req.Order,
		Owner:     owner,
		OrderType: orderType,
		DeferExec: req.DeferExec,
		PostOnly:  req.PostOnly,
	}

	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return PlaceMakerOrderResponse{}, requestBuildError("orders.place_maker_order", err)
	}
	headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
		Method:      http.MethodPost,
		RequestPath: "/order",
		Body:        string(bodyJSON),
	}, polyauth.Now())
	if err != nil {
		return PlaceMakerOrderResponse{}, authError("orders.place_maker_order", err)
	}

	body, err := c.transport.DoJSON(ctx, polyauth.TransportRequest{
		Op:      "orders.place_maker_order",
		Method:  http.MethodPost,
		Path:    "/order",
		Headers: headers.HTTPHeader(),
		Body:    bodyJSON,
	})
	if err != nil {
		return PlaceMakerOrderResponse{}, err
	}

	var resp PlaceMakerOrderResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return PlaceMakerOrderResponse{}, decodeError("orders.place_maker_order", body, err)
	}
	return resp, nil
}

func (c *Client) CancelOrder(ctx context.Context, req CancelOrderRequest) (CancelOrderResponse, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return CancelOrderResponse{}, err
	}
	if strings.TrimSpace(req.OrderID) == "" {
		return CancelOrderResponse{}, &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: "orders.cancel_order", Message: "orderID is required"}
	}

	payload := struct {
		OrderID string `json:"orderID"`
	}{OrderID: req.OrderID}

	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return CancelOrderResponse{}, requestBuildError("orders.cancel_order", err)
	}
	headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
		Method:      http.MethodDelete,
		RequestPath: "/order",
		Body:        string(bodyJSON),
	}, polyauth.Now())
	if err != nil {
		return CancelOrderResponse{}, authError("orders.cancel_order", err)
	}

	body, err := c.transport.DoJSON(ctx, polyauth.TransportRequest{
		Op:      "orders.cancel_order",
		Method:  http.MethodDelete,
		Path:    "/order",
		Headers: headers.HTTPHeader(),
		Body:    bodyJSON,
	})
	if err != nil {
		return CancelOrderResponse{}, err
	}

	var resp CancelOrderResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return CancelOrderResponse{}, decodeError("orders.cancel_order", body, err)
	}
	if resp.NotCanceled == nil {
		resp.NotCanceled = map[string]string{}
	}
	return resp, nil
}

func (c *Client) CancelAllOrders(ctx context.Context, req CancelAllOrdersRequest) (CancelAllOrdersResponse, error) {
	creds, err := c.credentials(ctx, req.Credentials)
	if err != nil {
		return CancelAllOrdersResponse{}, err
	}

	headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
		Method:      http.MethodDelete,
		RequestPath: "/cancel-all",
	}, polyauth.Now())
	if err != nil {
		return CancelAllOrdersResponse{}, authError("orders.cancel_all_orders", err)
	}

	body, err := c.transport.DoJSON(ctx, polyauth.TransportRequest{
		Op:      "orders.cancel_all_orders",
		Method:  http.MethodDelete,
		Path:    "/cancel-all",
		Headers: headers.HTTPHeader(),
	})
	if err != nil {
		return CancelAllOrdersResponse{}, err
	}

	var resp CancelAllOrdersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return CancelAllOrdersResponse{}, decodeError("orders.cancel_all_orders", body, err)
	}
	if resp.NotCanceled == nil {
		resp.NotCanceled = map[string]string{}
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

func (c *Client) getUserTradesPage(ctx context.Context, creds polyauth.APICredentials, nextCursor string, baseQuery url.Values, op string) ([]byte, error) {
	query := cloneQuery(baseQuery)
	query.Set("next_cursor", nextCursor)

	headers, err := c.signer.CreateL2Headers(creds, polyauth.L2HeaderArgs{
		Method:      http.MethodGet,
		RequestPath: "/data/trades",
	}, polyauth.Now())
	if err != nil {
		return nil, authError(op, err)
	}

	return c.transport.DoJSON(ctx, polyauth.TransportRequest{
		Op:      op,
		Method:  http.MethodGet,
		Path:    "/data/trades",
		Query:   query,
		Headers: headers.HTTPHeader(),
	})
}

func userTradesBaseQuery(id, makerAddress, market, assetID, before, after string) url.Values {
	query := url.Values{}
	setQueryIfPresent(query, "id", id)
	setQueryIfPresent(query, "maker_address", makerAddress)
	setQueryIfPresent(query, "market", market)
	setQueryIfPresent(query, "asset_id", assetID)
	setQueryIfPresent(query, "before", before)
	setQueryIfPresent(query, "after", after)
	return query
}

func setQueryIfPresent(query url.Values, key, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	query.Set(key, trimmed)
}

func cloneQuery(src url.Values) url.Values {
	dst := url.Values{}
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	return dst
}

func requestBuildError(op string, err error) error {
	return &polyerrors.Error{Kind: polyerrors.ErrRequestBuild, Op: op, Message: err.Error(), Cause: err}
}

func authError(op string, err error) error {
	return &polyerrors.Error{Kind: polyerrors.ErrAuth, Op: op, Message: err.Error(), Cause: err}
}

func decodeError(op string, body []byte, err error) error {
	return &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: op, Message: err.Error(), Cause: err, RawBody: body}
}
