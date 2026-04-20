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
	ClobTokenIDs             json.RawMessage `json:"clobTokenIds"`
	EndDate                  string          `json:"endDate"`
	CreatedAt                string          `json:"createdAt"`
	AcceptingOrdersTimestamp string          `json:"acceptingOrdersTimestamp"`
	EventStartTime           string          `json:"eventStartTime"`
	GroupItemTitle           string          `json:"groupItemTitle"`
}

type EventRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

func NewClient(httpClient *httpx.Client) *Client {
	return &Client{httpClient: httpClient}
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
