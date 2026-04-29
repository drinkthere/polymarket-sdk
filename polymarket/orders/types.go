package orders

import polyauth "github.com/drinkthere/polymarket-sdk/polymarket/auth"

type Side = polyauth.Side

const (
	SideBuy  = polyauth.SideBuy
	SideSell = polyauth.SideSell
)

type OrderType string

const (
	OrderTypeGTC OrderType = "GTC"
	OrderTypeGTD OrderType = "GTD"
	OrderTypeFOK OrderType = "FOK"
	OrderTypeFAK OrderType = "FAK"
)

type MakerOrder = polyauth.SignedOrder

type GetOpenOrdersRequest struct {
	Credentials polyauth.APICredentials
	ID          string
	Market      string
	AssetID     string
}

type OpenOrder struct {
	ID              string   `json:"id"`
	Status          string   `json:"status"`
	Owner           string   `json:"owner"`
	MakerAddress    string   `json:"maker_address"`
	Market          string   `json:"market"`
	AssetID         string   `json:"asset_id"`
	Side            Side     `json:"side"`
	OriginalSize    string   `json:"original_size"`
	SizeMatched     string   `json:"size_matched"`
	Price           string   `json:"price"`
	AssociateTrades []string `json:"associate_trades"`
	Outcome         string   `json:"outcome"`
	CreatedAt       int64    `json:"created_at"`
	Expiration      string   `json:"expiration"`
	OrderType       string   `json:"order_type"`
}

type GetUserTradesRequest struct {
	Credentials  polyauth.APICredentials
	ID           string
	MakerAddress string
	Market       string
	AssetID      string
	Before       string
	After        string
}

type GetUserTradesRawRequest struct {
	Credentials  polyauth.APICredentials
	ID           string
	MakerAddress string
	Market       string
	AssetID      string
	Before       string
	After        string
}

type UserTradeMakerOrder struct {
	OrderID       string `json:"order_id"`
	Owner         string `json:"owner"`
	MakerAddress  string `json:"maker_address"`
	MatchedAmount string `json:"matched_amount"`
	Price         string `json:"price"`
	FeeRateBPS    string `json:"fee_rate_bps"`
	AssetID       string `json:"asset_id"`
	Outcome       string `json:"outcome"`
	Side          Side   `json:"side"`
}

type UserTrade struct {
	ID              string                `json:"id"`
	TakerOrderID    string                `json:"taker_order_id"`
	AssetID         string                `json:"asset_id"`
	Market          string                `json:"market"`
	Side            Side                  `json:"side"`
	Price           string                `json:"price"`
	Size            string                `json:"size"`
	FeeRateBPS      string                `json:"fee_rate_bps"`
	Status          string                `json:"status"`
	MatchTime       string                `json:"match_time"`
	LastUpdate      string                `json:"last_update"`
	CreatedAt       string                `json:"created_at"`
	Owner           string                `json:"owner"`
	Outcome         string                `json:"outcome"`
	BucketIndex     int64                 `json:"bucket_index"`
	MakerAddress    string                `json:"maker_address"`
	TransactionHash string                `json:"transaction_hash"`
	TraderSide      string                `json:"trader_side"`
	MakerOrders     []UserTradeMakerOrder `json:"maker_orders"`
}

type PlaceMakerOrderRequest struct {
	Credentials polyauth.APICredentials
	Owner       string
	OrderType   OrderType
	Order       MakerOrder
	PostOnly    bool
	DeferExec   bool
}

type PlaceMakerOrderResponse struct {
	Success            bool     `json:"success"`
	ErrorMsg           string   `json:"errorMsg"`
	OrderID            string   `json:"orderID"`
	TransactionsHashes []string `json:"transactionsHashes"`
	Status             string   `json:"status"`
	TakingAmount       string   `json:"takingAmount"`
	MakingAmount       string   `json:"makingAmount"`
}

type CancelOrderRequest struct {
	Credentials polyauth.APICredentials
	OrderID     string
}

type CancelOrderResponse struct {
	Canceled    []string          `json:"canceled"`
	NotCanceled map[string]string `json:"not_canceled"`
}

type CancelAllOrdersRequest struct {
	Credentials polyauth.APICredentials
}

type CancelAllOrdersResponse struct {
	Canceled    []string          `json:"canceled"`
	NotCanceled map[string]string `json:"not_canceled"`
}
