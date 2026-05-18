package ctf

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

const (
	defaultRelayerBaseURL        = "https://relayer-v2.polymarket.com"
	defaultRelayerChainID        = int64(137)
	defaultDepositWalletDeadline = 20 * time.Minute
	relayerUserAgent             = "polymarket-sdk-go"
)

const erc20ApproveABIJSON = `[
  {"inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"name":"approve","outputs":[{"type":"bool"}],"stateMutability":"nonpayable","type":"function"}
]`

const collateralOnrampABIJSON = `[
  {"inputs":[{"name":"_asset","type":"address"},{"name":"_to","type":"address"},{"name":"_amount","type":"uint256"}],"name":"wrap","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

var (
	depositWalletDomainTypeHash = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	depositWalletCallTypeHash   = crypto.Keccak256Hash([]byte("Call(address target,uint256 value,bytes data)"))
	depositWalletBatchTypeHash  = crypto.Keccak256Hash([]byte("Batch(address wallet,uint256 nonce,uint256 deadline,Call[] calls)Call(address target,uint256 value,bytes data)"))
	depositWalletNameHash       = crypto.Keccak256Hash([]byte("DepositWallet"))
	depositWalletVersionHash    = crypto.Keccak256Hash([]byte("1"))
	erc20ApproveABI             = mustParseABI(erc20ApproveABIJSON)
	collateralOnrampABI         = mustParseABI(collateralOnrampABIJSON)
)

type DepositWalletRelayerClient struct {
	httpClient         *httpx.Client
	privateKey         *ecdsa.PrivateKey
	from               common.Address
	wallet             common.Address
	chainID            int64
	deadlineWindow     time.Duration
	collateralToken    common.Address
	builderCredentials BuilderCredentials
	relayerCredentials RelayerCredentials
}

type depositWalletCall struct {
	Target string `json:"target"`
	Value  string `json:"value"`
	Data   string `json:"data"`
}

type depositWalletBatchRequest struct {
	Type                string                   `json:"type"`
	From                string                   `json:"from"`
	To                  string                   `json:"to"`
	Nonce               string                   `json:"nonce"`
	Signature           string                   `json:"signature"`
	DepositWalletParams depositWalletBatchParams `json:"depositWalletParams"`
}

type depositWalletBatchParams struct {
	DepositWallet string              `json:"depositWallet"`
	Deadline      string              `json:"deadline"`
	Calls         []depositWalletCall `json:"calls"`
}

type relayerNonceResponse struct {
	Nonce string `json:"nonce"`
}

type relayerSubmitResponse struct {
	TransactionID   string `json:"transactionID"`
	State           string `json:"state"`
	Hash            string `json:"hash"`
	TransactionHash string `json:"transactionHash"`
}

func NewDepositWalletRelayerClientContext(ctx context.Context, cfg DepositWalletRelayerConfig) (*DepositWalletRelayerClient, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultRelayerBaseURL
	}
	httpClient, err := httpx.New(httpx.ClientConfig{
		BaseURL: baseURL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return NewDepositWalletRelayerClientWithHTTPClient(ctx, httpClient, cfg)
}

func NewDepositWalletRelayerClientWithHTTPClient(_ context.Context, httpClient *httpx.Client, cfg DepositWalletRelayerConfig) (*DepositWalletRelayerClient, error) {
	if httpClient == nil {
		return nil, requestError("ctf.deposit_wallet_relayer.new", errMissing("http client is required"))
	}
	privateKeyHex := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(cfg.PrivateKey), "0x"), "0X")
	if privateKeyHex == "" {
		return nil, requestError("ctf.deposit_wallet_relayer.new", errMissing("private_key is required"))
	}
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, requestError("ctf.deposit_wallet_relayer.new", err)
	}
	walletAddress := strings.TrimSpace(cfg.WalletAddress)
	if walletAddress == "" {
		return nil, requestError("ctf.deposit_wallet_relayer.new", errMissing("wallet_address is required"))
	}
	if !common.IsHexAddress(walletAddress) {
		return nil, requestError("ctf.deposit_wallet_relayer.new", errMissing("wallet_address must be a hex address"))
	}
	chainID := cfg.ChainID
	if chainID <= 0 {
		chainID = defaultRelayerChainID
	}
	deadlineWindow := cfg.DeadlineWindow
	if cfg.DeadlineWindowSeconds > 0 {
		deadlineWindow = time.Duration(cfg.DeadlineWindowSeconds) * time.Second
	}
	if deadlineWindow <= 0 {
		deadlineWindow = defaultDepositWalletDeadline
	}
	collateralToken := cfg.CollateralToken
	if collateralToken == (common.Address{}) {
		collateralToken = CTFCollateralAddress
	}
	return &DepositWalletRelayerClient{
		httpClient:         httpClient,
		privateKey:         privateKey,
		from:               crypto.PubkeyToAddress(privateKey.PublicKey),
		wallet:             common.HexToAddress(walletAddress),
		chainID:            chainID,
		deadlineWindow:     deadlineWindow,
		collateralToken:    collateralToken,
		builderCredentials: cfg.BuilderCredentials,
		relayerCredentials: cfg.RelayerCredentials,
	}, nil
}

func (c *DepositWalletRelayerClient) Close() error {
	return nil
}

func (c *DepositWalletRelayerClient) MergePositions(ctx context.Context, req MergePositionsRequest) (TransactionResult, error) {
	if c == nil || c.httpClient == nil || c.privateKey == nil {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.merge_positions", errMissing("relayer client is required"))
	}
	mergeReq := req
	if mergeReq.CollateralToken == (common.Address{}) {
		mergeReq.CollateralToken = c.collateralToken
	}
	target, data, err := BuildMergePositionsCalldata(mergeReq)
	if err != nil {
		return TransactionResult{}, err
	}
	call := depositWalletCall{
		Target: target.Hex(),
		Value:  "0",
		Data:   "0x" + hex.EncodeToString(data),
	}
	return c.submitWalletCalls(ctx, "ctf.deposit_wallet_relayer.merge_positions", []depositWalletCall{call}, target)
}

func (c *DepositWalletRelayerClient) MergePositionsAndWrapUSDCe(ctx context.Context, req MergePositionsRequest) (TransactionResult, error) {
	if c == nil || c.httpClient == nil || c.privateKey == nil {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.merge_positions_and_wrap_usdce", errMissing("relayer client is required"))
	}
	if req.Amount == nil || req.Amount.Sign() <= 0 {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.merge_positions_and_wrap_usdce", errMissing("amount must be > 0"))
	}
	mergeReq := req
	if mergeReq.CollateralToken == (common.Address{}) {
		mergeReq.CollateralToken = USDCeAddress
	}
	if mergeReq.CollateralToken != USDCeAddress {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.merge_positions_and_wrap_usdce", fmt.Errorf("collateral_token must be USDC.e for merge-and-wrap"))
	}
	target, data, err := BuildMergePositionsCalldata(mergeReq)
	if err != nil {
		return TransactionResult{}, err
	}
	activateCalls, err := c.wrapUSDCeCalls(WrapUSDCeRequest{Amount: req.Amount})
	if err != nil {
		return TransactionResult{}, err
	}
	calls := []depositWalletCall{{
		Target: target.Hex(),
		Value:  "0",
		Data:   "0x" + hex.EncodeToString(data),
	}}
	calls = append(calls, activateCalls...)
	return c.submitWalletCalls(ctx, "ctf.deposit_wallet_relayer.merge_positions_and_wrap_usdce", calls, target)
}

func (c *DepositWalletRelayerClient) WrapUSDCe(ctx context.Context, req WrapUSDCeRequest) (TransactionResult, error) {
	if c == nil || c.httpClient == nil || c.privateKey == nil {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.wrap_usdce", errMissing("relayer client is required"))
	}
	calls, err := c.wrapUSDCeCalls(req)
	if err != nil {
		return TransactionResult{}, err
	}
	return c.submitWalletCalls(ctx, "ctf.deposit_wallet_relayer.wrap_usdce", calls, CollateralOnrampAddress)
}

func (c *DepositWalletRelayerClient) RedeemPositions(ctx context.Context, req RedeemPositionsRequest) (TransactionResult, error) {
	if c == nil || c.httpClient == nil || c.privateKey == nil {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.redeem_positions", errMissing("relayer client is required"))
	}
	target, data, err := BuildRedeemPositionsCalldata(req)
	if err != nil {
		return TransactionResult{}, err
	}
	call := depositWalletCall{
		Target: target.Hex(),
		Value:  "0",
		Data:   "0x" + hex.EncodeToString(data),
	}
	return c.submitWalletCalls(ctx, "ctf.deposit_wallet_relayer.redeem_positions", []depositWalletCall{call}, target)
}

func (c *DepositWalletRelayerClient) NegRiskRedeem(ctx context.Context, req NegRiskRedeemRequest) (TransactionResult, error) {
	if c == nil || c.httpClient == nil || c.privateKey == nil {
		return TransactionResult{}, requestError("ctf.deposit_wallet_relayer.neg_risk_redeem", errMissing("relayer client is required"))
	}
	target, data, err := BuildNegRiskRedeemCalldata(req)
	if err != nil {
		return TransactionResult{}, err
	}
	call := depositWalletCall{
		Target: target.Hex(),
		Value:  "0",
		Data:   "0x" + hex.EncodeToString(data),
	}
	return c.submitWalletCalls(ctx, "ctf.deposit_wallet_relayer.neg_risk_redeem", []depositWalletCall{call}, target)
}

func (c *DepositWalletRelayerClient) wrapUSDCeCalls(req WrapUSDCeRequest) ([]depositWalletCall, error) {
	if req.Amount == nil || req.Amount.Sign() <= 0 {
		return nil, requestError("ctf.deposit_wallet_relayer.wrap_usdce", errMissing("amount must be > 0"))
	}
	to := req.To
	if to == (common.Address{}) {
		to = c.wallet
	}
	approveData, err := erc20ApproveABI.Pack("approve", CollateralOnrampAddress, req.Amount)
	if err != nil {
		return nil, requestError("ctf.deposit_wallet_relayer.wrap_usdce", err)
	}
	wrapData, err := collateralOnrampABI.Pack("wrap", USDCeAddress, to, req.Amount)
	if err != nil {
		return nil, requestError("ctf.deposit_wallet_relayer.wrap_usdce", err)
	}
	return []depositWalletCall{
		{
			Target: USDCeAddress.Hex(),
			Value:  "0",
			Data:   "0x" + hex.EncodeToString(approveData),
		},
		{
			Target: CollateralOnrampAddress.Hex(),
			Value:  "0",
			Data:   "0x" + hex.EncodeToString(wrapData),
		},
	}, nil
}

func (c *DepositWalletRelayerClient) submitWalletCalls(ctx context.Context, op string, calls []depositWalletCall, resultTarget common.Address) (TransactionResult, error) {
	if len(calls) == 0 {
		return TransactionResult{}, requestError(op, errMissing("calls are required"))
	}
	nonce, err := c.getNonce(ctx)
	if err != nil {
		return TransactionResult{}, err
	}
	deadline := strconv.FormatInt(time.Now().Add(c.deadlineWindow).Unix(), 10)
	signature, err := c.signBatch(nonce, deadline, calls)
	if err != nil {
		return TransactionResult{}, err
	}
	request := depositWalletBatchRequest{
		Type:      "WALLET",
		From:      c.from.Hex(),
		To:        DepositWalletFactory.Hex(),
		Nonce:     nonce,
		Signature: signature,
		DepositWalletParams: depositWalletBatchParams{
			DepositWallet: c.wallet.Hex(),
			Deadline:      deadline,
			Calls:         calls,
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return TransactionResult{}, requestError(op, err)
	}
	headers, err := c.relayerHeaders(http.MethodPost, "/submit", string(body))
	if err != nil {
		return TransactionResult{}, err
	}
	raw, err := c.httpClient.DoJSON(ctx, httpx.Request{
		Op:     "ctf.deposit_wallet_relayer.submit",
		Method: http.MethodPost,
		Path:   "/submit",
		Body:   json.RawMessage(body),
		Header: headers,
	})
	if err != nil {
		return TransactionResult{}, err
	}
	var resp relayerSubmitResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return TransactionResult{}, &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "ctf.deposit_wallet_relayer.submit", Message: err.Error(), Cause: err, RawBody: raw}
	}
	hash := strings.TrimSpace(resp.TransactionHash)
	if hash == "" {
		hash = strings.TrimSpace(resp.Hash)
	}
	if hash == "" {
		hash = strings.TrimSpace(resp.TransactionID)
	}
	return TransactionResult{
		Hash:   hash,
		Target: resultTarget,
	}, nil
}

func (c *DepositWalletRelayerClient) getNonce(ctx context.Context) (string, error) {
	query := url.Values{}
	query.Set("address", c.from.Hex())
	query.Set("type", "WALLET")
	headers, err := c.relayerHeaders(http.MethodGet, "/nonce", "")
	if err != nil {
		return "", err
	}
	raw, err := c.httpClient.DoJSON(ctx, httpx.Request{
		Op:     "ctf.deposit_wallet_relayer.nonce",
		Method: http.MethodGet,
		Path:   "/nonce",
		Query:  query,
		Header: headers,
	})
	if err != nil {
		return "", err
	}
	var resp relayerNonceResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", &polyerrors.Error{Kind: polyerrors.ErrDecode, Op: "ctf.deposit_wallet_relayer.nonce", Message: err.Error(), Cause: err, RawBody: raw}
	}
	nonce := strings.TrimSpace(resp.Nonce)
	if nonce == "" {
		return "", requestError("ctf.deposit_wallet_relayer.nonce", errMissing("empty nonce response"))
	}
	return nonce, nil
}

func (c *DepositWalletRelayerClient) signBatch(nonce string, deadline string, calls []depositWalletCall) (string, error) {
	nonceBig, ok := new(big.Int).SetString(nonce, 10)
	if !ok {
		return "", requestError("ctf.deposit_wallet_relayer.sign", fmt.Errorf("invalid nonce: %s", nonce))
	}
	deadlineBig, ok := new(big.Int).SetString(deadline, 10)
	if !ok {
		return "", requestError("ctf.deposit_wallet_relayer.sign", fmt.Errorf("invalid deadline: %s", deadline))
	}
	domainSeparator := crypto.Keccak256Hash(
		depositWalletDomainTypeHash.Bytes(),
		depositWalletNameHash.Bytes(),
		depositWalletVersionHash.Bytes(),
		uint256Bytes(big.NewInt(c.chainID)),
		addressBytes(c.wallet),
	)
	callHashes := make([]byte, 0, len(calls)*common.HashLength)
	for _, call := range calls {
		target := common.HexToAddress(call.Target)
		value, ok := new(big.Int).SetString(call.Value, 10)
		if !ok {
			return "", requestError("ctf.deposit_wallet_relayer.sign", fmt.Errorf("invalid call value: %s", call.Value))
		}
		data := common.FromHex(call.Data)
		callHash := crypto.Keccak256Hash(
			depositWalletCallTypeHash.Bytes(),
			addressBytes(target),
			uint256Bytes(value),
			crypto.Keccak256Hash(data).Bytes(),
		)
		callHashes = append(callHashes, callHash.Bytes()...)
	}
	callsHash := crypto.Keccak256Hash(callHashes)
	batchHash := crypto.Keccak256Hash(
		depositWalletBatchTypeHash.Bytes(),
		addressBytes(c.wallet),
		uint256Bytes(nonceBig),
		uint256Bytes(deadlineBig),
		callsHash.Bytes(),
	)
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), batchHash.Bytes())
	signature, err := crypto.Sign(digest.Bytes(), c.privateKey)
	if err != nil {
		return "", requestError("ctf.deposit_wallet_relayer.sign", err)
	}
	if signature[64] < 27 {
		signature[64] += 27
	}
	return "0x" + hex.EncodeToString(signature), nil
}

func (c *DepositWalletRelayerClient) relayerHeaders(method string, path string, body string) (http.Header, error) {
	headers := http.Header{}
	headers.Set("User-Agent", relayerUserAgent)
	if c.relayerCredentials.Valid() {
		headers.Set("RELAYER_API_KEY", c.relayerCredentials.Key)
		headers.Set("RELAYER_API_KEY_ADDRESS", c.relayerCredentials.KeyAddress)
	}
	if !c.builderCredentials.Valid() {
		return headers, nil
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	secret, err := base64.StdEncoding.DecodeString(c.builderCredentials.Secret)
	if err != nil {
		return nil, requestError("ctf.deposit_wallet_relayer.auth", err)
	}
	message := timestamp + method + path + body
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	signature = strings.ReplaceAll(signature, "+", "-")
	signature = strings.ReplaceAll(signature, "/", "_")
	headers.Set("POLY_BUILDER_API_KEY", c.builderCredentials.Key)
	headers.Set("POLY_BUILDER_PASSPHRASE", c.builderCredentials.Passphrase)
	headers.Set("POLY_BUILDER_SIGNATURE", signature)
	headers.Set("POLY_BUILDER_TIMESTAMP", timestamp)
	return headers, nil
}

func uint256Bytes(value *big.Int) []byte {
	if value == nil {
		return common.LeftPadBytes(nil, 32)
	}
	return common.LeftPadBytes(value.Bytes(), 32)
}

func addressBytes(value common.Address) []byte {
	return common.LeftPadBytes(value.Bytes(), 32)
}
