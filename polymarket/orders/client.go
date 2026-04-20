package orders

import (
	polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"
	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
)

type Client struct {
	httpClient *httpx.Client
	authConfig polyauth.Config
}

func NewClient(httpClient *httpx.Client, authConfig polyauth.Config) (*Client, error) {
	if httpClient == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "orders.new",
			Message: "http transport is required",
		}
	}
	if err := authConfig.Validate(); err != nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrAuth,
			Op:      "orders.new",
			Message: err.Error(),
			Cause:   err,
		}
	}
	return &Client{
		httpClient: httpClient,
		authConfig: authConfig,
	}, nil
}
