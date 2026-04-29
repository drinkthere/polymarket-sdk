package ctf

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

const testCTFABIJSON = `[
  {"inputs":[{"name":"conditionId","type":"bytes32"}],"name":"payoutDenominator","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const testNegRiskABIJSON = `[
  {"inputs":[{"name":"conditionId","type":"bytes32"},{"name":"amounts","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const testMergeABIJSON = `[
  {"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"},{"name":"amount","type":"uint256"}],"name":"mergePositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

func TestNewClientRequiresRPCURL(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrRequestBuild {
		t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
	}
	if typed.Op != "ctf.new" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestNewClientWithCallerRequiresCaller(t *testing.T) {
	var caller ContractCaller

	_, err := NewClientWithCaller(caller)
	if err == nil {
		t.Fatal("expected error")
	}

	var typed *polyerrors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}
	if typed.Kind != polyerrors.ErrRequestBuild {
		t.Fatalf("expected ErrRequestBuild, got %v", typed.Kind)
	}
	if typed.Op != "ctf.new" {
		t.Fatalf("unexpected op: %q", typed.Op)
	}
}

func TestClientCloseIsNoOpWithoutCloser(t *testing.T) {
	client, err := NewClientWithCaller(stubContractCaller{})
	if err != nil {
		t.Fatalf("NewClientWithCaller() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClientCloseDoesNotCloseInjectedCloser(t *testing.T) {
	caller := &stubClosableCaller{}

	client, err := NewClientWithCaller(caller)
	if err != nil {
		t.Fatalf("NewClientWithCaller() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if caller.closed {
		t.Fatal("expected injected closer to remain open")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if caller.closeCalls != 0 {
		t.Fatalf("close calls = %d want 0", caller.closeCalls)
	}
}

func TestClientCloseUsesOwnedCloser(t *testing.T) {
	closeCalls := 0

	client, err := newClient(stubContractCaller{}, func() error {
		closeCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d want 1", closeCalls)
	}
}

func TestIsResolvedReturnsTrueWhenPayoutDenominatorPositive(t *testing.T) {
	expectedCondition := "0x1234"
	caller := stubContractCaller{
		callContract: func(msg ethereum.CallMsg) ([]byte, error) {
			if msg.To == nil {
				t.Fatal("expected call target")
			}
			if *msg.To != CTFContractAddress {
				t.Fatalf("target = %s", msg.To.Hex())
			}
			assertSelector(t, msg.Data, "payoutDenominator(bytes32)")

			method := mustTestABI(t, testCTFABIJSON).Methods["payoutDenominator"]
			args, err := method.Inputs.Unpack(msg.Data[4:])
			if err != nil {
				t.Fatalf("unpack inputs: %v", err)
			}
			if len(args) != 1 {
				t.Fatalf("expected 1 arg, got %d", len(args))
			}
			if got := args[0].([32]byte); got != conditionIDToHash(expectedCondition) {
				t.Fatalf("condition = %x", got)
			}

			outputs, err := method.Outputs.Pack(big.NewInt(1))
			if err != nil {
				t.Fatalf("pack outputs: %v", err)
			}
			return outputs, nil
		},
	}

	client, err := NewClientWithCaller(caller)
	if err != nil {
		t.Fatalf("NewClientWithCaller() error = %v", err)
	}

	resolved, err := client.IsResolved(t.Context(), expectedCondition)
	if err != nil {
		t.Fatalf("IsResolved() error = %v", err)
	}
	if !resolved {
		t.Fatal("expected resolved")
	}
}

func TestIsResolvedReturnsFalseWhenPayoutDenominatorZero(t *testing.T) {
	caller := stubContractCaller{
		callContract: func(_ ethereum.CallMsg) ([]byte, error) {
			outputs, err := mustTestABI(t, testCTFABIJSON).Methods["payoutDenominator"].Outputs.Pack(big.NewInt(0))
			if err != nil {
				t.Fatalf("pack outputs: %v", err)
			}
			return outputs, nil
		},
	}

	client, err := NewClientWithCaller(caller)
	if err != nil {
		t.Fatalf("NewClientWithCaller() error = %v", err)
	}

	resolved, err := client.IsResolved(t.Context(), "1234")
	if err != nil {
		t.Fatalf("IsResolved() error = %v", err)
	}
	if resolved {
		t.Fatal("expected unresolved")
	}
}

func TestBuildRedeemPositionsCalldataBuildsCallDataAndTarget(t *testing.T) {
	target, data, err := BuildRedeemPositionsCalldata(RedeemPositionsRequest{
		CollateralToken:    USDCAddress,
		ParentCollectionID: common.Hash{},
		ConditionID:        "0x1234",
		IndexSets:          []uint64{1, 2},
	})
	if err != nil {
		t.Fatalf("BuildRedeemPositionsCalldata() error = %v", err)
	}
	if target != CTFContractAddress {
		t.Fatalf("target = %s", target.Hex())
	}
	if len(data) == 0 {
		t.Fatal("expected calldata")
	}

	assertSelector(t, data, "redeemPositions(address,bytes32,bytes32,uint256[])")
	method := mustTestABI(t, testCTFABIJSON).Methods["redeemPositions"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack inputs: %v", err)
	}
	if got := args[0].(common.Address); got != USDCAddress {
		t.Fatalf("collateral = %s", got.Hex())
	}
	if got := args[1].([32]byte); got != (common.Hash{}) {
		t.Fatalf("parent collection = %x", got)
	}
	if got := args[2].([32]byte); got != conditionIDToHash("0x1234") {
		t.Fatalf("condition = %x", got)
	}
	assertBigIntSlice(t, args[3].([]*big.Int), []uint64{1, 2})
}

func TestBuildRedeemPositionsCalldataUsesLegacyUSDCOverrideForZeroCollateral(t *testing.T) {
	legacy := USDCTokenAddress
	modern := USDCAddress
	override := common.HexToAddress("0x1111111111111111111111111111111111111111")
	USDCTokenAddress = override
	defer func() {
		USDCTokenAddress = legacy
		USDCAddress = modern
	}()

	_, data, err := BuildRedeemPositionsCalldata(RedeemPositionsRequest{
		ConditionID: "0x1234",
		IndexSets:   []uint64{1, 2},
	})
	if err != nil {
		t.Fatalf("BuildRedeemPositionsCalldata() error = %v", err)
	}

	method := mustTestABI(t, testCTFABIJSON).Methods["redeemPositions"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack inputs: %v", err)
	}
	if got := args[0].(common.Address); got != override {
		t.Fatalf("collateral = %s want %s", got.Hex(), override.Hex())
	}
}

func TestBuildRedeemPositionsCalldataUsesUSDCAddressOverrideForZeroCollateral(t *testing.T) {
	legacy := USDCTokenAddress
	modern := USDCAddress
	override := common.HexToAddress("0x2222222222222222222222222222222222222222")
	USDCAddress = override
	defer func() {
		USDCTokenAddress = legacy
		USDCAddress = modern
	}()

	_, data, err := BuildRedeemPositionsCalldata(RedeemPositionsRequest{
		ConditionID: "0x1234",
		IndexSets:   []uint64{1, 2},
	})
	if err != nil {
		t.Fatalf("BuildRedeemPositionsCalldata() error = %v", err)
	}

	method := mustTestABI(t, testCTFABIJSON).Methods["redeemPositions"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack inputs: %v", err)
	}
	if got := args[0].(common.Address); got != override {
		t.Fatalf("collateral = %s want %s", got.Hex(), override.Hex())
	}
}

func TestBuildRedeemPositionsCalldataPrefersLegacyOverrideWhenBothDiverge(t *testing.T) {
	legacy := USDCTokenAddress
	modern := USDCAddress
	legacyOverride := common.HexToAddress("0x3333333333333333333333333333333333333333")
	modernOverride := common.HexToAddress("0x4444444444444444444444444444444444444444")
	USDCTokenAddress = legacyOverride
	USDCAddress = modernOverride
	defer func() {
		USDCTokenAddress = legacy
		USDCAddress = modern
	}()

	_, data, err := BuildRedeemPositionsCalldata(RedeemPositionsRequest{
		ConditionID: "0x1234",
		IndexSets:   []uint64{1, 2},
	})
	if err != nil {
		t.Fatalf("BuildRedeemPositionsCalldata() error = %v", err)
	}

	method := mustTestABI(t, testCTFABIJSON).Methods["redeemPositions"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack inputs: %v", err)
	}
	if got := args[0].(common.Address); got != legacyOverride {
		t.Fatalf("collateral = %s want %s", got.Hex(), legacyOverride.Hex())
	}
}

func TestBuildNegRiskRedeemCalldataBuildsCallDataAndTarget(t *testing.T) {
	target, data, err := BuildNegRiskRedeemCalldata(NegRiskRedeemRequest{
		ConditionID: "0xabcdef",
		Amounts: []*big.Int{
			big.NewInt(100),
			big.NewInt(250),
		},
	})
	if err != nil {
		t.Fatalf("BuildNegRiskRedeemCalldata() error = %v", err)
	}
	if target != NegRiskAdapterAddress {
		t.Fatalf("target = %s", target.Hex())
	}
	if len(data) == 0 {
		t.Fatal("expected calldata")
	}

	assertSelector(t, data, "redeemPositions(bytes32,uint256[])")
	method := mustTestABI(t, testNegRiskABIJSON).Methods["redeemPositions"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack inputs: %v", err)
	}
	if got := args[0].([32]byte); got != conditionIDToHash("0xabcdef") {
		t.Fatalf("condition = %x", got)
	}
	if got := args[1].([]*big.Int); len(got) != 2 || got[0].Cmp(big.NewInt(100)) != 0 || got[1].Cmp(big.NewInt(250)) != 0 {
		t.Fatalf("amounts = %v", got)
	}
}

func TestBuildMergePositionsCalldataBuildsCallDataAndTarget(t *testing.T) {
	target, data, err := BuildMergePositionsCalldata(MergePositionsRequest{
		CollateralToken:    USDCAddress,
		ParentCollectionID: common.Hash{},
		ConditionID:        "1234",
		IndexSets:          []uint64{1, 2},
		Amount:             big.NewInt(5_000_000),
	})
	if err != nil {
		t.Fatalf("BuildMergePositionsCalldata() error = %v", err)
	}
	if target != CTFContractAddress {
		t.Fatalf("target = %s", target.Hex())
	}
	if len(data) == 0 {
		t.Fatal("expected calldata")
	}

	assertSelector(t, data, "mergePositions(address,bytes32,bytes32,uint256[],uint256)")
	method := mustTestABI(t, testMergeABIJSON).Methods["mergePositions"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack inputs: %v", err)
	}
	if got := args[0].(common.Address); got != USDCAddress {
		t.Fatalf("collateral = %s", got.Hex())
	}
	if got := args[1].([32]byte); got != (common.Hash{}) {
		t.Fatalf("parent collection = %x", got)
	}
	if got := args[2].([32]byte); got != conditionIDToHash("1234") {
		t.Fatalf("condition = %x", got)
	}
	assertBigIntSlice(t, args[3].([]*big.Int), []uint64{1, 2})
	if got := args[4].(*big.Int); got.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("amount = %s", got)
	}
}

type stubContractCaller struct {
	callContract func(msg ethereum.CallMsg) ([]byte, error)
}

func (s stubContractCaller) CallContract(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	if s.callContract == nil {
		return nil, nil
	}
	return s.callContract(msg)
}

type stubClosableCaller struct {
	closed     bool
	closeCalls int
}

func (s *stubClosableCaller) CallContract(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	return nil, nil
}

func (s *stubClosableCaller) Close() error {
	s.closed = true
	s.closeCalls++
	return nil
}

func mustTestABI(t *testing.T, raw string) abi.ABI {
	t.Helper()

	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("abi.JSON() error = %v", err)
	}
	return parsed
}

func assertSelector(t *testing.T, data []byte, signature string) {
	t.Helper()

	if len(data) < 4 {
		t.Fatalf("calldata too short: %d", len(data))
	}
	want := crypto.Keccak256([]byte(signature))[:4]
	if got := data[:4]; !bytes.Equal(got, want) {
		t.Fatalf("selector = %x want %x", got, want)
	}
}

func assertBigIntSlice(t *testing.T, got []*big.Int, want []uint64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len = %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Cmp(new(big.Int).SetUint64(want[i])) != 0 {
			t.Fatalf("got[%d] = %s want %d", i, got[i], want[i])
		}
	}
}
