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

func TestNewClientRequiresCaller(t *testing.T) {
	var caller ContractCaller

	_, err := NewClient(caller)
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

	client, err := NewClient(caller)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
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

	client, err := NewClient(caller)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resolved, err := client.IsResolved(t.Context(), "1234")
	if err != nil {
		t.Fatalf("IsResolved() error = %v", err)
	}
	if resolved {
		t.Fatal("expected unresolved")
	}
}

func TestRedeemPositionsBuildsCallDataAndTarget(t *testing.T) {
	target, data, err := RedeemPositions(RedeemPositionsRequest{
		CollateralToken:    USDCTokenAddress,
		ParentCollectionID: common.Hash{},
		ConditionID:        "0x1234",
		IndexSets:          []uint64{1, 2},
	})
	if err != nil {
		t.Fatalf("RedeemPositions() error = %v", err)
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
	if got := args[0].(common.Address); got != USDCTokenAddress {
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

func TestNegRiskRedeemBuildsCallDataAndTarget(t *testing.T) {
	target, data, err := NegRiskRedeem(NegRiskRedeemRequest{
		ConditionID: "0xabcdef",
		Amounts: []*big.Int{
			big.NewInt(100),
			big.NewInt(250),
		},
	})
	if err != nil {
		t.Fatalf("NegRiskRedeem() error = %v", err)
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

func TestMergePositionsBuildsCallDataAndTarget(t *testing.T) {
	target, data, err := MergePositions(MergePositionsRequest{
		CollateralToken:    USDCTokenAddress,
		ParentCollectionID: common.Hash{},
		ConditionID:        "1234",
		IndexSets:          []uint64{1, 2},
		Amount:             big.NewInt(5_000_000),
	})
	if err != nil {
		t.Fatalf("MergePositions() error = %v", err)
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
	if got := args[0].(common.Address); got != USDCTokenAddress {
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
	return s.callContract(msg)
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
