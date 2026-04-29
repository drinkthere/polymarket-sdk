package balances

import polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"

const (
	AssetTypeCollateral  = "COLLATERAL"
	AssetTypeConditional = "CONDITIONAL"
)

type GetBalanceRequest struct {
	Credentials   polyauth.APICredentials
	AssetType     string
	TokenID       string
	SignatureType int
}

type GetBalanceResponse struct {
	Balance   string `json:"balance"`
	Allowance string `json:"allowance"`
}

type UpdateAllowanceRequest struct {
	Credentials   polyauth.APICredentials
	AssetType     string
	TokenID       string
	SignatureType int
}

type UpdateAllowanceResponse struct {
	Updated bool `json:"updated"`
}
