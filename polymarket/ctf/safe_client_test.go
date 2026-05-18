package ctf

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestSafeTransactionClientMergePositionsSubmitsThroughSafe(t *testing.T) {
	privateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	safe := common.HexToAddress("0x2222222222222222222222222222222222222222")
	backend := &stubSafeTransactionBackend{}

	client, err := NewSafeTransactionClientWithBackend(t.Context(), backend, nil, SafeTransactionConfig{
		PrivateKey:              privateKey,
		SafeAddress:             safe.Hex(),
		ChainID:                 137,
		ReceiptPollAttempts:     1,
		ReceiptPollIntervalMS:   1,
		ReceiptConfirmationWait: true,
	})
	if err != nil {
		t.Fatalf("NewSafeTransactionClientWithBackend: %v", err)
	}

	result, err := client.MergePositions(t.Context(), MergePositionsRequest{
		ConditionID: validConditionID,
		IndexSets:   []uint64{1, 2},
		Amount:      big.NewInt(2_000_000),
	})
	if err != nil {
		t.Fatalf("MergePositions: %v", err)
	}
	if result.Hash == "" {
		t.Fatal("expected transaction hash")
	}
	if result.Target != CTFContractAddress {
		t.Fatalf("target = %s want %s", result.Target.Hex(), CTFContractAddress.Hex())
	}
	if len(backend.sent) != 1 {
		t.Fatalf("sent tx count = %d want 1", len(backend.sent))
	}
	tx := backend.sent[0]
	if tx.To() == nil || *tx.To() != safe {
		t.Fatalf("tx target = %+v want safe %s", tx.To(), safe.Hex())
	}
	assertSelector(t, tx.Data(), "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)")
}

type stubSafeTransactionBackend struct {
	sent []*ethtypes.Transaction
}

func (s *stubSafeTransactionBackend) CallContract(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	if msg.To == nil {
		return nil, nil
	}
	switch {
	case hasSelector(msg.Data, "nonce()"):
		return safeABI.Methods["nonce"].Outputs.Pack(big.NewInt(3))
	case hasSelector(msg.Data, "getTransactionHash(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,uint256)"):
		hash := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
		return safeABI.Methods["getTransactionHash"].Outputs.Pack(hash)
	default:
		return nil, nil
	}
}

func (s *stubSafeTransactionBackend) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return 7, nil
}

func (s *stubSafeTransactionBackend) SuggestGasPrice(context.Context) (*big.Int, error) {
	return big.NewInt(1_000_000_000), nil
}

func (s *stubSafeTransactionBackend) EstimateGas(context.Context, ethereum.CallMsg) (uint64, error) {
	return 100_000, nil
}

func (s *stubSafeTransactionBackend) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(1_000_000_000_000_000_000), nil
}

func (s *stubSafeTransactionBackend) SendTransaction(_ context.Context, tx *ethtypes.Transaction) error {
	s.sent = append(s.sent, tx)
	return nil
}

func (s *stubSafeTransactionBackend) TransactionReceipt(context.Context, common.Hash) (*ethtypes.Receipt, error) {
	return &ethtypes.Receipt{Status: ethtypes.ReceiptStatusSuccessful, GasUsed: 100_000, BlockNumber: big.NewInt(123)}, nil
}

func (s *stubSafeTransactionBackend) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(137), nil
}

func hasSelector(data []byte, signature string) bool {
	if len(data) < 4 {
		return false
	}
	want := crypto.Keccak256([]byte(signature))[:4]
	return string(data[:4]) == string(want)
}
