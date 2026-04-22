package gamma

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

const settlementBySlugOp = "gamma.settlement_by_slug"

type Outcome string

const (
	OutcomeUnsettled Outcome = "UNSETTLED"
	OutcomeYes       Outcome = "YES"
	OutcomeNo        Outcome = "NO"
)

type Client struct {
	httpClient *httpx.Client
}

type event struct {
	Markets []market `json:"markets"`
}

type market struct {
	Closed        bool   `json:"closed"`
	OutcomePrices string `json:"outcomePrices"`
}

func NewClient(httpClient *httpx.Client) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "gamma.new",
			Message: "http transport is required",
		}
	}
	return &Client{httpClient: httpClient}, nil
}

func (c *Client) GetSettlementBySlug(ctx context.Context, slug string) (Outcome, error) {
	query := url.Values{}
	query.Set("slug", slug)

	payload, err := c.httpClient.DoJSON(ctx, httpx.Request{
		Op:     settlementBySlugOp,
		Method: http.MethodGet,
		Path:   "/events",
		Query:  query,
	})
	if err != nil {
		return OutcomeUnsettled, err
	}

	var events []event
	if err := json.Unmarshal(payload, &events); err != nil {
		return OutcomeUnsettled, decodeError(settlementBySlugOp, payload, err)
	}

	for _, event := range events {
		for _, market := range event.Markets {
			if !market.Closed {
				return OutcomeUnsettled, nil
			}

			var prices []string
			if err := json.Unmarshal([]byte(market.OutcomePrices), &prices); err != nil {
				return OutcomeUnsettled, decodeError(settlementBySlugOp, []byte(market.OutcomePrices), err)
			}
			if len(prices) == 2 && prices[0] == "1" && prices[1] == "0" {
				return OutcomeYes, nil
			}
			if len(prices) == 2 && prices[0] == "0" && prices[1] == "1" {
				return OutcomeNo, nil
			}
		}
	}

	return OutcomeUnsettled, nil
}

func decodeError(op string, rawBody []byte, err error) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrDecode,
		Op:      op,
		Method:  http.MethodGet,
		Message: err.Error(),
		Cause:   err,
		RawBody: rawBody,
	}
}
