package ws

import (
	"encoding/json"
	"strings"
)

// Envelope captures the minimal Polymarket-style websocket message envelope fields
// used to infer message kinds across endpoints.
type Envelope struct {
	Type    string `json:"type"`
	Event   string `json:"event"`
	Channel string `json:"channel"`
}

// InferMessageType returns a best-effort message type string extracted from common
// websocket envelope fields. If payload is not JSON, it returns an empty string.
func InferMessageType(payload []byte) string {
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return ""
	}
	if s := strings.TrimSpace(env.Type); s != "" {
		return s
	}
	if s := strings.TrimSpace(env.Event); s != "" {
		return s
	}
	return strings.TrimSpace(env.Channel)
}
