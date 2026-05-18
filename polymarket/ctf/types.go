package ctf

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	canonicalUSDCAddress    = common.HexToAddress("0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB")
	CTFCollateralAddress    = common.HexToAddress("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174")
	CTFContractAddress      = common.HexToAddress("0x4D97DCd97eC945f40cF65F87097ACe5EA0476045")
	NegRiskAdapterAddress   = common.HexToAddress("0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296")
	DepositWalletFactory    = common.HexToAddress("0x00000000000Fb5C9ADea0298D729A0CB3823Cc07")
	CollateralOnrampAddress = common.HexToAddress("0x93070a847efEf7F70739046A929D47a521F5B8ee")
	USDCeAddress            = CTFCollateralAddress
	USDCAddress             = canonicalUSDCAddress
	USDCTokenAddress        = canonicalUSDCAddress
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

type SafeTransactionConfig struct {
	RPCURL                  string
	PrivateKey              string
	SafeAddress             string
	ChainID                 int64
	GasLimit                uint64
	ReceiptPollAttempts     int
	ReceiptPollInterval     time.Duration
	ReceiptPollIntervalMS   int
	ReceiptConfirmationWait bool
}

type BuilderCredentials struct {
	Key        string
	Secret     string
	Passphrase string
}

func (c BuilderCredentials) Valid() bool {
	return c.Key != "" && c.Secret != "" && c.Passphrase != ""
}

type RelayerCredentials struct {
	Key        string
	KeyAddress string
}

func (c RelayerCredentials) Valid() bool {
	return c.Key != "" && c.KeyAddress != ""
}

type DepositWalletRelayerConfig struct {
	BaseURL               string
	PrivateKey            string
	WalletAddress         string
	ChainID               int64
	DeadlineWindow        time.Duration
	DeadlineWindowSeconds int
	CollateralToken       common.Address
	BuilderCredentials    BuilderCredentials
	RelayerCredentials    RelayerCredentials
}

type WrapUSDCeRequest struct {
	Amount *big.Int
	To     common.Address
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
