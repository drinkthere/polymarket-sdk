package ctf

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	canonicalUSDCAddress  = common.HexToAddress("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174")
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
