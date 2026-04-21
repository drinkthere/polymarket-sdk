package markets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

type CryptoPriceRequest struct {
	Symbol         string
	Variant        string
	EventStartTime time.Time
	EndDate        time.Time
}

type CryptoPriceResponse struct {
	OpenPrice float64 `json:"openPrice"`
}

func (c *Client) GetCryptoPriceOpen(ctx context.Context, req CryptoPriceRequest) (CryptoPriceResponse, error) {
	query := url.Values{}
	query.Set("symbol", strings.ToUpper(strings.TrimSpace(req.Symbol)))
	query.Set("variant", strings.TrimSpace(req.Variant))
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
