package ctf

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

const ctfABIJSON = `[
  {"inputs":[{"name":"conditionId","type":"bytes32"}],"name":"payoutDenominator","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"conditionId","type":"bytes32"},{"name":"index","type":"uint256"}],"name":"payoutNumerators","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const negRiskABIJSON = `[
  {"inputs":[{"name":"conditionId","type":"bytes32"},{"name":"amounts","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const mergeCtfABIJSON = `[
  {"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"},{"name":"amount","type":"uint256"}],"name":"mergePositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const defaultTransactionGasLimit uint64 = 350000

var (
	ctfABI        = mustParseABI(ctfABIJSON)
	negRiskABI    = mustParseABI(negRiskABIJSON)
	mergeABI      = mustParseABI(mergeCtfABIJSON)
	dialRPCClient = func(ctx context.Context, rpcURL string) (ContractCaller, func() error, error) {
		eth, err := ethclient.DialContext(ctx, rpcURL)
		if err != nil {
			return nil, nil, err
		}
		return eth, func() error {
			eth.Close()
			return nil
		}, nil
	}
)

type Client struct {
	caller    ContractCaller
	closeFn   func() error
	closeOnce sync.Once
	closeErr  error
}

type TransactionBackend interface {
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	ChainID(ctx context.Context) (*big.Int, error)
}

type TransactionClient struct {
	backend    TransactionBackend
	privateKey *ecdsa.PrivateKey
	from       common.Address
	chainID    *big.Int
	gasLimit   uint64
	closeFn    func() error
	closeOnce  sync.Once
	closeErr   error
}

func NewClient(cfg Config) (*Client, error) {
	return NewClientContext(context.Background(), cfg)
}

func NewClientContext(ctx context.Context, cfg Config) (*Client, error) {
	rpcURL := strings.TrimSpace(cfg.RPCURL)
	if rpcURL == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.new",
			Message: "rpc_url is required",
		}
	}

	caller, closeFn, err := dialRPCClient(ctx, rpcURL)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.new",
			Message: err.Error(),
			Cause:   err,
		}
	}

	return newClient(caller, closeFn)
}

func NewClientWithCaller(caller ContractCaller) (*Client, error) {
	return newClient(caller, nil)
}

func NewTransactionClientContext(ctx context.Context, cfg TransactionConfig) (*TransactionClient, error) {
	rpcURL := strings.TrimSpace(cfg.RPCURL)
	if rpcURL == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.tx.new",
			Message: "rpc_url is required",
		}
	}
	eth, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.tx.new",
			Message: err.Error(),
			Cause:   err,
		}
	}
	return NewTransactionClientWithBackend(ctx, eth, func() error {
		eth.Close()
		return nil
	}, cfg)
}

func NewTransactionClientWithBackend(ctx context.Context, backend TransactionBackend, closeFn func() error, cfg TransactionConfig) (*TransactionClient, error) {
	if backend == nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.tx.new",
			Message: "transaction backend is required",
		}
	}
	privateKeyHex := strings.TrimSpace(cfg.PrivateKey)
	privateKeyHex = strings.TrimPrefix(strings.TrimPrefix(privateKeyHex, "0x"), "0X")
	if privateKeyHex == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.tx.new",
			Message: "private_key is required",
		}
	}
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.tx.new",
			Message: err.Error(),
			Cause:   err,
		}
	}
	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID <= 0 {
		var err error
		chainID, err = backend.ChainID(ctx)
		if err != nil {
			return nil, &polyerrors.Error{
				Kind:    classifyRPCError(err),
				Op:      "ctf.tx.new",
				Message: err.Error(),
				Cause:   err,
			}
		}
	}
	gasLimit := cfg.GasLimit
	if gasLimit == 0 {
		gasLimit = defaultTransactionGasLimit
	}
	return &TransactionClient{
		backend:    backend,
		privateKey: privateKey,
		from:       crypto.PubkeyToAddress(privateKey.PublicKey),
		chainID:    chainID,
		gasLimit:   gasLimit,
		closeFn:    closeFn,
	}, nil
}

func newClient(caller ContractCaller, closeFn func() error) (*Client, error) {
	if isNilContractCaller(caller) {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.new",
			Message: "contract caller is required",
		}
	}

	return &Client{
		caller:  caller,
		closeFn: closeFn,
	}, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	c.closeOnce.Do(func() {
		if c.closeFn != nil {
			c.closeErr = c.closeFn()
		}
	})

	return c.closeErr
}

func (c *TransactionClient) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.closeFn != nil {
			c.closeErr = c.closeFn()
		}
	})
	return c.closeErr
}

func (c *TransactionClient) MergePositions(ctx context.Context, req MergePositionsRequest) (TransactionResult, error) {
	target, data, err := BuildMergePositionsCalldata(req)
	if err != nil {
		return TransactionResult{}, err
	}
	tx, err := c.buildSignedTransaction(ctx, target, data)
	if err != nil {
		return TransactionResult{}, err
	}
	if err := c.backend.SendTransaction(ctx, tx); err != nil {
		return TransactionResult{}, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.tx.merge_positions",
			Message: err.Error(),
			Cause:   err,
		}
	}
	return TransactionResult{
		Hash:   tx.Hash().Hex(),
		Target: target,
	}, nil
}

func (c *TransactionClient) buildSignedTransaction(ctx context.Context, target common.Address, data []byte) (*types.Transaction, error) {
	if c == nil || c.backend == nil || c.privateKey == nil {
		return nil, requestError("ctf.tx.build", errMissing("transaction client is required"))
	}
	nonce, err := c.backend.PendingNonceAt(ctx, c.from)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.tx.build",
			Message: err.Error(),
			Cause:   err,
		}
	}
	gasPrice, err := c.backend.SuggestGasPrice(ctx)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.tx.build",
			Message: err.Error(),
			Cause:   err,
		}
	}
	gasLimit := c.gasLimit
	if gasLimit == 0 {
		estimated, err := c.backend.EstimateGas(ctx, ethereum.CallMsg{
			From: c.from,
			To:   &target,
			Data: data,
		})
		if err != nil {
			return nil, &polyerrors.Error{
				Kind:    classifyRPCError(err),
				Op:      "ctf.tx.build",
				Message: err.Error(),
				Cause:   err,
			}
		}
		gasLimit = estimated
	}
	tx := types.NewTransaction(nonce, target, big.NewInt(0), gasLimit, gasPrice, data)
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(c.chainID), c.privateKey)
	if err != nil {
		return nil, requestError("ctf.tx.build", err)
	}
	return signed, nil
}

func (c *Client) IsResolved(ctx context.Context, conditionID string) (bool, error) {
	conditionHash, err := parseConditionID(conditionID)
	if err != nil {
		return false, requestError("ctf.is_resolved", err)
	}

	data, err := ctfABI.Pack("payoutDenominator", conditionHash)
	if err != nil {
		return false, requestError("ctf.is_resolved", err)
	}

	target := CTFContractAddress
	raw, err := c.caller.CallContract(ctx, ethereum.CallMsg{
		To:   &target,
		Data: data,
	}, nil)
	if err != nil {
		return false, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.is_resolved",
			Message: err.Error(),
			Cause:   err,
		}
	}

	out, err := ctfABI.Unpack("payoutDenominator", raw)
	if err != nil {
		return false, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "ctf.is_resolved",
			Message: err.Error(),
			Cause:   err,
			RawBody: raw,
		}
	}
	if len(out) != 1 {
		return false, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "ctf.is_resolved",
			Message: "unexpected payoutDenominator response",
			RawBody: raw,
		}
	}

	denominator, ok := out[0].(*big.Int)
	if !ok {
		return false, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "ctf.is_resolved",
			Message: "unexpected payoutDenominator type",
			RawBody: raw,
		}
	}

	return denominator.Sign() > 0, nil
}

func (c *Client) GetPayoutNumerator(ctx context.Context, conditionID string, outcomeIndex uint64) (*big.Int, error) {
	conditionHash, err := parseConditionID(conditionID)
	if err != nil {
		return nil, requestError("ctf.payout_numerator", err)
	}

	data, err := ctfABI.Pack("payoutNumerators", conditionHash, new(big.Int).SetUint64(outcomeIndex))
	if err != nil {
		return nil, requestError("ctf.payout_numerator", err)
	}

	target := CTFContractAddress
	raw, err := c.caller.CallContract(ctx, ethereum.CallMsg{
		To:   &target,
		Data: data,
	}, nil)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.payout_numerator",
			Message: err.Error(),
			Cause:   err,
		}
	}

	out, err := ctfABI.Unpack("payoutNumerators", raw)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "ctf.payout_numerator",
			Message: err.Error(),
			Cause:   err,
			RawBody: raw,
		}
	}
	if len(out) != 1 {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "ctf.payout_numerator",
			Message: "unexpected payoutNumerators response",
			RawBody: raw,
		}
	}

	numerator, ok := out[0].(*big.Int)
	if !ok {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrDecode,
			Op:      "ctf.payout_numerator",
			Message: "unexpected payoutNumerators type",
			RawBody: raw,
		}
	}

	return new(big.Int).Set(numerator), nil
}

func BuildRedeemPositionsCalldata(req RedeemPositionsRequest) (common.Address, []byte, error) {
	conditionHash, indexSets, collateral, err := validateRedeemRequest(req)
	if err != nil {
		return common.Address{}, nil, err
	}

	data, err := ctfABI.Pack(
		"redeemPositions",
		collateral,
		req.ParentCollectionID,
		conditionHash,
		indexSets,
	)
	if err != nil {
		return common.Address{}, nil, requestError("ctf.redeem_positions", err)
	}

	return CTFContractAddress, data, nil
}

func BuildNegRiskRedeemCalldata(req NegRiskRedeemRequest) (common.Address, []byte, error) {
	conditionHash, err := parseConditionID(req.ConditionID)
	if err != nil {
		return common.Address{}, nil, requestError("ctf.neg_risk_redeem", err)
	}
	if len(req.Amounts) == 0 {
		return common.Address{}, nil, requestError("ctf.neg_risk_redeem", errMissing("amounts is required"))
	}
	for i, amount := range req.Amounts {
		if amount == nil {
			return common.Address{}, nil, requestError("ctf.neg_risk_redeem", errMissing("amounts["+strconv.Itoa(i)+"] is required"))
		}
		if amount.Sign() < 0 {
			return common.Address{}, nil, requestError("ctf.neg_risk_redeem", errMissing("amounts["+strconv.Itoa(i)+"] must be non-negative"))
		}
	}

	data, err := negRiskABI.Pack("redeemPositions", conditionHash, req.Amounts)
	if err != nil {
		return common.Address{}, nil, requestError("ctf.neg_risk_redeem", err)
	}

	return NegRiskAdapterAddress, data, nil
}

func BuildMergePositionsCalldata(req MergePositionsRequest) (common.Address, []byte, error) {
	conditionHash, indexSets, collateral, err := validateMergeRequest(req)
	if err != nil {
		return common.Address{}, nil, err
	}

	data, err := mergeABI.Pack(
		"mergePositions",
		collateral,
		req.ParentCollectionID,
		conditionHash,
		indexSets,
		req.Amount,
	)
	if err != nil {
		return common.Address{}, nil, requestError("ctf.merge_positions", err)
	}

	return CTFContractAddress, data, nil
}

func RedeemPositions(req RedeemPositionsRequest) (common.Address, []byte, error) {
	return BuildRedeemPositionsCalldata(req)
}

func NegRiskRedeem(req NegRiskRedeemRequest) (common.Address, []byte, error) {
	return BuildNegRiskRedeemCalldata(req)
}

func MergePositions(req MergePositionsRequest) (common.Address, []byte, error) {
	return BuildMergePositionsCalldata(req)
}

func validateRedeemRequest(req RedeemPositionsRequest) (common.Hash, []*big.Int, common.Address, error) {
	conditionHash, err := parseConditionID(req.ConditionID)
	if err != nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.redeem_positions", err)
	}
	indexSets, err := validateIndexSets(req.IndexSets, 1, false)
	if err != nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.redeem_positions", err)
	}

	return conditionHash, indexSets, defaultCollateral(req.CollateralToken), nil
}

func validateMergeRequest(req MergePositionsRequest) (common.Hash, []*big.Int, common.Address, error) {
	conditionHash, err := parseConditionID(req.ConditionID)
	if err != nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", err)
	}
	indexSets, err := validateIndexSets(req.IndexSets, 2, true)
	if err != nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", err)
	}
	if req.Amount == nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", errMissing("amount is required"))
	}
	if req.Amount.Sign() < 0 {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", errMissing("amount must be non-negative"))
	}

	return conditionHash, indexSets, defaultCollateral(req.CollateralToken), nil
}

func defaultCollateral(collateral common.Address) common.Address {
	if collateral == (common.Address{}) {
		legacyOverridden := USDCTokenAddress != canonicalUSDCAddress
		modernOverridden := USDCAddress != canonicalUSDCAddress

		switch {
		case legacyOverridden && !modernOverridden:
			return USDCTokenAddress
		case modernOverridden && !legacyOverridden:
			return USDCAddress
		case legacyOverridden && modernOverridden:
			return USDCTokenAddress
		default:
			return canonicalUSDCAddress
		}
	}
	return collateral
}

func uint64SliceToBigInts(values []uint64) []*big.Int {
	out := make([]*big.Int, len(values))
	for i, value := range values {
		out[i] = new(big.Int).SetUint64(value)
	}
	return out
}

func conditionIDToHash(conditionID string) [32]byte {
	hash, _ := parseConditionID(conditionID)
	return [32]byte(hash)
}

func parseConditionID(conditionID string) (common.Hash, error) {
	normalized := strings.TrimSpace(conditionID)
	if normalized == "" {
		return common.Hash{}, errMissing("condition_id is required")
	}
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "0x"), "0X")
	if normalized == "" {
		return common.Hash{}, errMissing("condition_id is required")
	}
	if len(normalized) != common.HashLength*2 {
		return common.Hash{}, errMissing("condition_id must be exactly 32 bytes (64 hex chars)")
	}

	decoded, err := hex.DecodeString(normalized)
	if err != nil {
		return common.Hash{}, err
	}

	return common.BytesToHash(decoded), nil
}

func validateIndexSets(indexSets []uint64, minCount int, requireDisjoint bool) ([]*big.Int, error) {
	if len(indexSets) < minCount {
		if minCount == 1 {
			return nil, errMissing("index_sets is required")
		}
		return nil, errMissing("at least two index_sets are required")
	}

	out := make([]*big.Int, len(indexSets))
	for i, indexSet := range indexSets {
		if indexSet == 0 {
			return nil, errMissing("index_sets must be > 0")
		}
		if requireDisjoint {
			for j := 0; j < i; j++ {
				if indexSet&indexSets[j] != 0 {
					return nil, errMissing("index_sets must be unique and non-overlapping")
				}
			}
		}
		out[i] = new(big.Int).SetUint64(indexSet)
	}

	return out, nil
}

func mustParseABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

func requestError(op string, err error) error {
	return &polyerrors.Error{
		Kind:    polyerrors.ErrRequestBuild,
		Op:      op,
		Message: err.Error(),
		Cause:   err,
	}
}

func errMissing(message string) error {
	return simpleError(message)
}

func isNilContractCaller(caller ContractCaller) bool {
	if caller == nil {
		return true
	}

	value := reflect.ValueOf(caller)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func classifyRPCError(err error) polyerrors.ErrorKind {
	switch {
	case errors.Is(err, context.Canceled):
		return polyerrors.ErrClosed
	case errors.Is(err, context.DeadlineExceeded) || isNetTimeoutErr(err):
		return polyerrors.ErrTimeout
	case isRequestBuildRPCError(err):
		return polyerrors.ErrRequestBuild
	case isClosedRPCError(err):
		return polyerrors.ErrClosed
	default:
		return polyerrors.ErrNetwork
	}
}

func isNetTimeoutErr(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isRequestBuildRPCError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "no known transport for url scheme") ||
		strings.Contains(message, "unsupported protocol scheme") ||
		strings.Contains(message, "missing protocol scheme") ||
		strings.Contains(message, "first path segment in url cannot contain colon") ||
		strings.HasPrefix(message, "parse ")
}

func isClosedRPCError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "client is closed") ||
		strings.Contains(message, "connection is closed") ||
		strings.Contains(message, "use of closed network connection")
}

type simpleError string

func (e simpleError) Error() string {
	return string(e)
}
