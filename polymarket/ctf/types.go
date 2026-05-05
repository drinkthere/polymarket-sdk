package ctf

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	canonicalUSDCAddress  = common.HexToAddress("0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB")
	CTFContractAddress    = common.HexToAddress("0x4D97DCd97eC945f40cF65F87097ACe5EA0476045")
	NegRiskAdapterAddress = common.HexToAddress("0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296")
	USDCAddress           = canonicalUSDCAddress
	USDCTokenAddress      = canonicalUSDCAddress
)

type ContractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

type Config struct {
	RPCURL string
}

type TransactionConfig struct {
	RPCURL     string
	PrivateKey string
	ChainID    int64
	GasLimit   uint64
}

type TransactionResult struct {
	Hash   string
	Target common.Address
}

type RedeemPositionsRequest struct {
	CollateralToken    common.Address
	ParentCollectionID common.Hash
	ConditionID        string
	IndexSets          []uint64
}

type NegRiskRedeemRequest struct {
	ConditionID string
	Amounts     []*big.Int
}

type MergePositionsRequest struct {
	CollateralToken    common.Address
	ParentCollectionID common.Hash
	ConditionID        string
	IndexSets          []uint64
	Amount             *big.Int
}
