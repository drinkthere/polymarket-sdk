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
	"github.com/ethereum/go-ethereum/common"
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
	user, err := validateUser(req.User, listOp)
	if err != nil {
		return ListResponse{}, err
	}
	if req.Limit < 0 {
		return ListResponse{}, requestBuildError(listOp, "limit must be >= 0")
	}
	if req.Limit > defaultPageLimit {
		return ListResponse{}, requestBuildError(listOp, "limit must be <= 500")
	}
	if req.Offset < 0 {
		return ListResponse{}, requestBuildError(listOp, "offset must be >= 0")
	}
	if req.Offset > maxPageOffset {
		return ListResponse{}, requestBuildError(listOp, "offset must be <= 10000")
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
	user, err := validateUser(pageReq.User, listAllOp)
	if err != nil {
		return ListResponse{}, err
	}
	pageReq.User = user
	if pageReq.Limit > defaultPageLimit {
		return ListResponse{}, requestBuildError(listAllOp, "limit must be <= 500")
	}
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
		return ListResponse{}, requestBuildError(listAllOp, "offset must be <= 10000")
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
			return ListResponse{}, requestBuildError(listAllOp, "offset must be <= 10000")
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

func validateUser(user string, op string) (string, error) {
	trimmed := strings.TrimSpace(user)
	if trimmed == "" {
		return "", requestBuildError(op, "user is required")
	}
	if !common.IsHexAddress(trimmed) {
		return "", requestBuildError(op, "user must be a valid hex address")
	}
	return trimmed, nil
}

func requestBuildError(op string, message string) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrRequestBuild,
		Op:      op,
		Message: message,
	}
}
