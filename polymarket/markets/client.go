package markets

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

type Client struct {
	httpClient *httpx.Client
}

type ListRequest struct {
	Active bool
	Closed bool
	Limit  int
	Offset int
}

type ListResponse struct {
	Markets []Market
}

type GetEventsBySlugResponse struct {
	Events []Event
}

type GetClobMarketInfoResponse struct {
	ConditionID string          `json:"condition_id"`
	GST         *string         `json:"gst"`
	R           json.RawMessage `json:"r"`
	T           []ClobToken     `json:"t"`
	MOS         float64         `json:"mos"`
	MTS         float64         `json:"mts"`
	MBF         float64         `json:"mbf"`
	TBF         float64         `json:"tbf"`
	RFQE        bool            `json:"rfqe"`
	ITODE       bool            `json:"itode"`
	IBCE        bool            `json:"ibce"`
	FD          FeeDetails      `json:"fd"`
	OAS         float64         `json:"oas"`
}

type ClobToken struct {
	TokenID string `json:"t"`
	Outcome string `json:"o"`
}

type FeeDetails struct {
	Rate      float64 `json:"r"`
	Exponent  float64 `json:"e"`
	TakerOnly bool    `json:"to"`
}

type Event struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	StartTime    string   `json:"startTime"`
	StartDate    string   `json:"startDate"`
	CreationDate string   `json:"creationDate"`
	EndDate      string   `json:"endDate"`
	Markets      []Market `json:"markets"`
}

type Market struct {
	ID                       string          `json:"id"`
	EventID                  string          `json:"eventId"`
	Events                   []EventRef      `json:"events"`
	ConditionID              string          `json:"conditionId"`
	Slug                     string          `json:"slug"`
	Question                 string          `json:"question"`
	Description              string          `json:"description"`
	Outcomes                 json.RawMessage `json:"outcomes"`
	OutcomePrices            json.RawMessage `json:"outcomePrices"`
	ClobTokenIDs             json.RawMessage `json:"clobTokenIds"`
	EndDate                  string          `json:"endDate"`
	CreatedAt                string          `json:"createdAt"`
	AcceptingOrdersTimestamp string          `json:"acceptingOrdersTimestamp"`
	EventStartTime           string          `json:"eventStartTime"`
	GroupItemTitle           string          `json:"groupItemTitle"`
	SportsMarketType         string          `json:"sportsMarketType"`
	Active                   bool            `json:"active"`
	Closed                   bool            `json:"closed"`
	BestBid                  float64         `json:"bestBid"`
	BestAsk                  float64         `json:"bestAsk"`
	Spread                   float64         `json:"spread"`
}

type EventRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

func NewClient(httpClient *httpx.Client) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "markets.new",
			Message: "http transport is required",
		}
	}
	return &Client{httpClient: httpClient}, nil
}

func (c *Client) ListMarkets(ctx context.Context, req ListRequest) (ListResponse, error) {
	query := url.Values{}
	query.Set("active", strconv.FormatBool(req.Active))
	query.Set("closed", strconv.FormatBool(req.Closed))
	if req.Limit > 0 {
		query.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		query.Set("offset", strconv.Itoa(req.Offset))
	} else {
		query.Set("offset", "0")
	}

	var markets []Market
	if err := c.do(ctx, "markets.list", "/markets", query, &markets); err != nil {
		return ListResponse{}, err
	}
	return ListResponse{Markets: markets}, nil
}

func (c *Client) GetEventsBySlug(ctx context.Context, slug string) (GetEventsBySlugResponse, error) {
	query := url.Values{}
	query.Set("slug", slug)

	var events []Event
	if err := c.do(ctx, "events.get_by_slug", "/events", query, &events); err != nil {
		return GetEventsBySlugResponse{}, err
	}
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" {
			return GetEventsBySlugResponse{}, protocolError("events.get_by_slug", "event id is required")
		}
	}
	return GetEventsBySlugResponse{Events: events}, nil
}

func (c *Client) GetClobMarketInfo(ctx context.Context, conditionID string) (GetClobMarketInfoResponse, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return GetClobMarketInfoResponse{}, requestBuildError("markets.get_clob_market_info", "conditionID is required")
	}

	var info GetClobMarketInfoResponse
	if err := c.do(ctx, "markets.get_clob_market_info", "/markets/"+conditionID, nil, &info); err != nil {
		return GetClobMarketInfoResponse{}, err
	}
	if strings.TrimSpace(info.ConditionID) == "" {
		info.ConditionID = conditionID
	}
	if len(info.T) == 0 {
		return GetClobMarketInfoResponse{}, protocolError("markets.get_clob_market_info", "tokens are required")
	}
	if info.MTS <= 0 {
		return GetClobMarketInfoResponse{}, protocolError("markets.get_clob_market_info", "mts must be > 0")
	}
	if info.MOS <= 0 {
		return GetClobMarketInfoResponse{}, protocolError("markets.get_clob_market_info", "mos must be > 0")
	}
	return info, nil
}

func (c *Client) GetCryptoPriceOpen(ctx context.Context, req CryptoPriceRequest) (CryptoPriceResponse, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		return CryptoPriceResponse{}, requestBuildError("crypto_price.get_open", "symbol is required")
	}

	variant := strings.TrimSpace(req.Variant)
	if variant == "" {
		return CryptoPriceResponse{}, requestBuildError("crypto_price.get_open", "variant is required")
	}
	if req.EventStartTime.IsZero() {
		return CryptoPriceResponse{}, requestBuildError("crypto_price.get_open", "eventStartTime is required")
	}
	if req.EndDate.IsZero() {
		return CryptoPriceResponse{}, requestBuildError("crypto_price.get_open", "endDate is required")
	}

	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("variant", variant)
	query.Set("eventStartTime", req.EventStartTime.UTC().Format("2006-01-02T15:04:05Z"))
	query.Set("endDate", req.EndDate.UTC().Format("2006-01-02T15:04:05Z"))

	payload, err := c.httpClient.DoJSON(ctx, httpx.Request{
		Op:     "crypto_price.get_open",
		Method: http.MethodGet,
		Path:   "/api/crypto/crypto-price",
		Query:  query,
	})
	if err != nil {
		return CryptoPriceResponse{}, err
	}

	var out CryptoPriceResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return CryptoPriceResponse{}, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "crypto_price.get_open",
			Method:  http.MethodGet,
			Message: err.Error(),
			Cause:   err,
			RawBody: payload,
		}
	}
	if out.OpenPrice <= 0 {
		return CryptoPriceResponse{}, protocolError("crypto_price.get_open", "openPrice must be > 0")
	}
	return out, nil
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
