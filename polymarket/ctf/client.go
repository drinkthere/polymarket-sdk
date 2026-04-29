package ctf

import (
	"context"
	"encoding/hex"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

const ctfABIJSON = `[
  {"inputs":[{"name":"conditionId","type":"bytes32"}],"name":"payoutDenominator","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const negRiskABIJSON = `[
  {"inputs":[{"name":"conditionId","type":"bytes32"},{"name":"amounts","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const mergeCtfABIJSON = `[
  {"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"},{"name":"amount","type":"uint256"}],"name":"mergePositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

var (
	ctfABI     = mustParseABI(ctfABIJSON)
	negRiskABI = mustParseABI(negRiskABIJSON)
	mergeABI   = mustParseABI(mergeCtfABIJSON)
)

type Client struct {
	caller ContractCaller
}

func NewClient(cfg Config) (*Client, error) {
	rpcURL := strings.TrimSpace(cfg.RPCURL)
	if rpcURL == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.new",
			Message: "rpc_url is required",
		}
	}

	eth, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrNetwork,
			Op:      "ctf.new",
			Message: err.Error(),
			Cause:   err,
		}
	}

	return NewClientWithCaller(eth)
}

func NewClientWithCaller(caller ContractCaller) (*Client, error) {
	if isNilContractCaller(caller) {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.new",
			Message: "contract caller is required",
		}
	}

	return &Client{caller: caller}, nil
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
			Kind:    polyerrors.ErrNetwork,
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
	if len(req.IndexSets) == 0 {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.redeem_positions", errMissing("index_sets is required"))
	}

	return conditionHash, uint64SliceToBigInts(req.IndexSets), defaultCollateral(req.CollateralToken), nil
}

func validateMergeRequest(req MergePositionsRequest) (common.Hash, []*big.Int, common.Address, error) {
	conditionHash, err := parseConditionID(req.ConditionID)
	if err != nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", err)
	}
	if len(req.IndexSets) == 0 {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", errMissing("index_sets is required"))
	}
	if req.Amount == nil {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", errMissing("amount is required"))
	}
	if req.Amount.Sign() < 0 {
		return common.Hash{}, nil, common.Address{}, requestError("ctf.merge_positions", errMissing("amount must be non-negative"))
	}

	return conditionHash, uint64SliceToBigInts(req.IndexSets), defaultCollateral(req.CollateralToken), nil
}

func defaultCollateral(collateral common.Address) common.Address {
	if collateral == (common.Address{}) {
		return USDCAddress
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
	if len(normalized)%2 == 1 {
		normalized = "0" + normalized
	}

	decoded, err := hex.DecodeString(normalized)
	if err != nil {
		return common.Hash{}, err
	}
	if len(decoded) > common.HashLength {
		return common.Hash{}, errMissing("condition_id exceeds 32 bytes")
	}

	return common.BytesToHash(decoded), nil
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

type simpleError string

func (e simpleError) Error() string {
	return string(e)
}
