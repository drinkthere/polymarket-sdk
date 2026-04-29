package positions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

const (
	listOp               = "positions.list"
	listAllOp            = "positions.list_all"
	defaultPageLimit     = 500
	defaultSizeThreshold = "0"
	maxPageOffset        = 10000
)

type Client struct {
	httpClient *httpx.Client
}

func NewClient(httpClient *httpx.Client) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "positions.new",
			Message: "http transport is required",
		}
	}
	return &Client{httpClient: httpClient}, nil
}

func (c *Client) List(ctx context.Context, req ListRequest) (ListResponse, error) {
	user := strings.TrimSpace(req.User)
	if user == "" {
		return ListResponse{}, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      listOp,
			Message: "user is required",
		}
	}

	query := url.Values{}
	query.Set("user", user)
	if req.Redeemable != nil {
		query.Set("redeemable", strconv.FormatBool(*req.Redeemable))
	}
	if req.Mergeable != nil {
		query.Set("mergeable", strconv.FormatBool(*req.Mergeable))
	}
	if req.Limit > 0 {
		query.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		query.Set("offset", strconv.Itoa(req.Offset))
	} else {
		query.Set("offset", "0")
	}
	if threshold := strings.TrimSpace(req.SizeThreshold); threshold != "" {
		query.Set("sizeThreshold", threshold)
	}

	var positions []Position
	if err := c.do(ctx, listOp, "/positions", query, &positions); err != nil {
		return ListResponse{}, err
	}
	return ListResponse{Positions: positions}, nil
}

func (c *Client) ListAll(ctx context.Context, req ListRequest) (ListResponse, error) {
	pageReq := req
	if pageReq.Limit <= 0 {
		pageReq.Limit = defaultPageLimit
	}
	if pageReq.Offset < 0 {
		pageReq.Offset = 0
	}
	if strings.TrimSpace(pageReq.SizeThreshold) == "" {
		pageReq.SizeThreshold = defaultSizeThreshold
	}
	if pageReq.Offset > maxPageOffset {
		return ListResponse{}, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      listAllOp,
			Message: "offset must be <= 10000",
		}
	}

	var all []Position
	for {
		resp, err := c.List(ctx, pageReq)
		if err != nil {
			return ListResponse{}, err
		}

		all = append(all, resp.Positions...)
		if len(resp.Positions) < pageReq.Limit {
			break
		}

		nextOffset := pageReq.Offset + len(resp.Positions)
		if nextOffset > maxPageOffset {
			return ListResponse{}, &polyerrors.Error{
				Kind:    polyerrors.ErrRequestBuild,
				Op:      listAllOp,
				Message: "offset must be <= 10000",
			}
		}
		pageReq.Offset = nextOffset
	}

	return ListResponse{Positions: all}, nil
}

func (c *Client) do(ctx context.Context, op string, path string, query url.Values, out any) error {
	payload, err := c.httpClient.DoJSON(ctx, httpx.Request{
		Op:     op,
		Method: http.MethodGet,
		Path:   path,
		Query:  query,
	})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      op,
			Method:  http.MethodGet,
			Message: err.Error(),
			Cause:   err,
			RawBody: payload,
		}
	}
	return nil
}
