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
	sizeThreshold, err := normalizeSizeThreshold(req.SizeThreshold, listOp)
	if err != nil {
		return ListResponse{}, err
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
	if sizeThreshold != "" {
		query.Set("sizeThreshold", sizeThreshold)
	}

	var positions []Position
	if err := c.do(ctx, listOp, "/positions", query, &positions); err != nil {
		return ListResponse{}, err
	}
	if err := validatePositions(listOp, positions); err != nil {
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
	if pageReq.Limit < 0 {
		return ListResponse{}, requestBuildError(listAllOp, "limit must be >= 0")
	}
	if pageReq.Limit == 0 {
		pageReq.Limit = defaultPageLimit
	}
	if pageReq.Offset < 0 {
		return ListResponse{}, requestBuildError(listAllOp, "offset must be >= 0")
	}
	sizeThreshold, err := normalizeSizeThreshold(pageReq.SizeThreshold, listAllOp)
	if err != nil {
		return ListResponse{}, err
	}
	if sizeThreshold == "" {
		sizeThreshold = defaultSizeThreshold
	}
	pageReq.SizeThreshold = sizeThreshold
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
	if !isStrictHexAddress(trimmed) {
		return "", requestBuildError(op, "user must be a valid hex address")
	}
	return trimmed, nil
}

func normalizeSizeThreshold(raw string, op string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !isNonNegativeNumericString(trimmed) {
		return "", requestBuildError(op, "sizeThreshold must be a non-negative numeric string")
	}
	return trimmed, nil
}

func isStrictHexAddress(raw string) bool {
	if len(raw) != 42 || !strings.HasPrefix(raw, "0x") {
		return false
	}
	for i := 2; i < len(raw); i++ {
		if !isHexChar(raw[i]) {
			return false
		}
	}
	return true
}

func isHexChar(ch byte) bool {
	return ('0' <= ch && ch <= '9') ||
		('a' <= ch && ch <= 'f') ||
		('A' <= ch && ch <= 'F')
}

func isNonNegativeNumericString(raw string) bool {
	if raw == "" || raw[0] == '-' || raw[0] == '+' {
		return false
	}

	hasDigit := false
	dotCount := 0
	for i := 0; i < len(raw); i++ {
		switch ch := raw[i]; {
		case '0' <= ch && ch <= '9':
			hasDigit = true
		case ch == '.':
			dotCount++
			if dotCount > 1 {
				return false
			}
		default:
			return false
		}
	}
	if !hasDigit {
		return false
	}

	_, err := strconv.ParseFloat(raw, 64)
	return err == nil
}

func validatePositions(op string, positions []Position) error {
	for i, position := range positions {
		if strings.TrimSpace(position.Asset) == "" {
			return protocolError(op, "positions["+strconv.Itoa(i)+"].asset is required")
		}
		if strings.TrimSpace(position.ConditionID) == "" {
			return protocolError(op, "positions["+strconv.Itoa(i)+"].conditionId is required")
		}
		if position.OutcomeIndex < 0 {
			return protocolError(op, "positions["+strconv.Itoa(i)+"].outcomeIndex must be >= 0")
		}
	}
	return nil
}

func protocolError(op string, message string) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrProtocol,
		Op:      op,
		Method:  http.MethodGet,
		Message: message,
	}
}

func requestBuildError(op string, message string) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrRequestBuild,
		Op:      op,
		Message: message,
	}
}
