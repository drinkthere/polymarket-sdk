package rtds

import (
	"encoding/json"
	"strings"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

type Update struct {
	Topic          string
	Type           string
	Timestamp      int64
	CryptoPrice    *PricePayload
	ChainlinkPrice *PricePayload
	EquityPrice    *EquityPricePayload
	EquitySnapshot *EquitySnapshotPayload
}

type PricePayload struct {
	Symbol    string  `json:"symbol"`
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type EquityPricePayload struct {
	Symbol            string  `json:"symbol"`
	Value             float64 `json:"value"`
	FullAccuracyValue string  `json:"full_accuracy_value"`
	Timestamp         int64   `json:"timestamp"`
	ReceivedAt        int64   `json:"received_at,omitempty"`
	IsCarriedForward  bool    `json:"is_carried_forward,omitempty"`
}

type EquitySnapshotPayload struct {
	Symbol string                `json:"symbol"`
	Data   []EquitySnapshotPoint `json:"data"`
}

type EquitySnapshotPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

func DecodeUpdate(raw []byte) (Update, error) {
	var env struct {
		Topic     string          `json:"topic"`
		Type      string          `json:"type"`
		Timestamp int64           `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return Update{}, decodeError("rtds.decode", raw, err)
	}

	out := Update{
		Topic:     env.Topic,
		Type:      env.Type,
		Timestamp: env.Timestamp,
	}

	switch strings.TrimSpace(env.Topic) {
	case "crypto_prices":
		var payload PricePayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return Update{}, decodeError("rtds.decode_crypto_price", env.Payload, err)
		}
		out.CryptoPrice = &payload
	case "crypto_prices_chainlink":
		var payload PricePayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return Update{}, decodeError("rtds.decode_chainlink_price", env.Payload, err)
		}
		out.ChainlinkPrice = &payload
	case "equity_prices":
		if strings.TrimSpace(env.Type) == "subscribe" {
			var payload EquitySnapshotPayload
			if err := json.Unmarshal(env.Payload, &payload); err != nil {
				return Update{}, decodeError("rtds.decode_equity_snapshot", env.Payload, err)
			}
			out.EquitySnapshot = &payload
			break
		}

		var payload EquityPricePayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return Update{}, decodeError("rtds.decode_equity_price", env.Payload, err)
		}
		out.EquityPrice = &payload
	}

	return out, nil
}

func decodeError(op string, raw []byte, err error) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrDecode,
		Op:      op,
		Message: err.Error(),
		Cause:   err,
		RawBody: raw,
	}
}
