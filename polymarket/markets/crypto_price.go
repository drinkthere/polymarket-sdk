package markets

import "time"

type CryptoPriceRequest struct {
	Symbol         string
	Variant        string
	EventStartTime time.Time
	EndDate        time.Time
}

type CryptoPriceResponse struct {
	OpenPrice float64 `json:"openPrice"`
}
