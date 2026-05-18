package ctf

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	polyerrors "github.com/drinkthere/polymarket-sdk/polymarket/errors"
)

const gnosisSafeABIJSON = `[
  {"inputs":[],"name":"nonce","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"operation","type":"uint8"},{"name":"safeTxGas","type":"uint256"},{"name":"baseGas","type":"uint256"},{"name":"gasPrice","type":"uint256"},{"name":"gasToken","type":"address"},{"name":"refundReceiver","type":"address"},{"name":"_nonce","type":"uint256"}],"name":"getTransactionHash","outputs":[{"type":"bytes32"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"operation","type":"uint8"},{"name":"safeTxGas","type":"uint256"},{"name":"baseGas","type":"uint256"},{"name":"gasPrice","type":"uint256"},{"name":"gasToken","type":"address"},{"name":"refundReceiver","type":"address"},{"name":"signatures","type":"bytes"}],"name":"execTransaction","outputs":[{"type":"bool"}],"stateMutability":"payable","type":"function"}
]`

const (
	defaultSafeTransactionReceiptPollAttempts = 90
	defaultSafeTransactionReceiptPollInterval = 2 * time.Second
)

var safeABI = mustParseABI(gnosisSafeABIJSON)

type SafeTransactionBackend interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	ChainID(ctx context.Context) (*big.Int, error)
}

type SafeTransactionClient struct {
	backend             SafeTransactionBackend
	privateKey          *ecdsa.PrivateKey
	from                common.Address
	safe                common.Address
	chainID             *big.Int
	gasLimit            uint64
	receiptPollAttempts int
	receiptPollInterval time.Duration
	closeFn             func() error
	closeOnce           sync.Once
	closeErr            error
	txMu                sync.Mutex
}

func NewSafeTransactionClientContext(ctx context.Context, cfg SafeTransactionConfig) (*SafeTransactionClient, error) {
	rpcURL := strings.TrimSpace(cfg.RPCURL)
	if rpcURL == "" {
		return nil, &polyerrors.Error{
			Kind:    polyerrors.ErrRequestBuild,
			Op:      "ctf.safe_tx.new",
			Message: "rpc_url is required",
		}
	}
	eth, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.new",
			Message: err.Error(),
			Cause:   err,
		}
	}
	return NewSafeTransactionClientWithBackend(ctx, eth, func() error {
		eth.Close()
		return nil
	}, cfg)
}

func NewSafeTransactionClientWithBackend(ctx context.Context, backend SafeTransactionBackend, closeFn func() error, cfg SafeTransactionConfig) (*SafeTransactionClient, error) {
	if backend == nil {
		return nil, requestError("ctf.safe_tx.new", errMissing("transaction backend is required"))
	}
	privateKeyHex := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(cfg.PrivateKey), "0x"), "0X")
	if privateKeyHex == "" {
		return nil, requestError("ctf.safe_tx.new", errMissing("private_key is required"))
	}
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, requestError("ctf.safe_tx.new", err)
	}
	safeAddress := strings.TrimSpace(cfg.SafeAddress)
	if safeAddress == "" {
		return nil, requestError("ctf.safe_tx.new", errMissing("safe_address is required"))
	}
	if !common.IsHexAddress(safeAddress) {
		return nil, requestError("ctf.safe_tx.new", errMissing("safe_address must be a hex address"))
	}
	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID <= 0 {
		chainID, err = backend.ChainID(ctx)
		if err != nil {
			return nil, &polyerrors.Error{
				Kind:    classifyRPCError(err),
				Op:      "ctf.safe_tx.new",
				Message: err.Error(),
				Cause:   err,
			}
		}
	}
	receiptPollAttempts := cfg.ReceiptPollAttempts
	if receiptPollAttempts <= 0 {
		receiptPollAttempts = defaultSafeTransactionReceiptPollAttempts
	}
	receiptPollInterval := cfg.ReceiptPollInterval
	if cfg.ReceiptPollIntervalMS > 0 {
		receiptPollInterval = time.Duration(cfg.ReceiptPollIntervalMS) * time.Millisecond
	}
	if receiptPollInterval <= 0 {
		receiptPollInterval = defaultSafeTransactionReceiptPollInterval
	}
	return &SafeTransactionClient{
		backend:             backend,
		privateKey:          privateKey,
		from:                crypto.PubkeyToAddress(privateKey.PublicKey),
		safe:                common.HexToAddress(safeAddress),
		chainID:             chainID,
		gasLimit:            cfg.GasLimit,
		receiptPollAttempts: receiptPollAttempts,
		receiptPollInterval: receiptPollInterval,
		closeFn:             closeFn,
	}, nil
}

func (c *SafeTransactionClient) Close() error {
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

func (c *SafeTransactionClient) MergePositions(ctx context.Context, req MergePositionsRequest) (TransactionResult, error) {
	if c == nil || c.backend == nil || c.privateKey == nil {
		return TransactionResult{}, requestError("ctf.safe_tx.merge_positions", errMissing("transaction client is required"))
	}
	target, data, err := BuildMergePositionsCalldata(req)
	if err != nil {
		return TransactionResult{}, err
	}

	c.txMu.Lock()
	defer c.txMu.Unlock()

	tx, err := c.buildSignedSafeTransaction(ctx, target, data, 0)
	if err != nil {
		return TransactionResult{}, err
	}
	if err := c.backend.SendTransaction(ctx, tx); err != nil {
		return TransactionResult{}, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.merge_positions",
			Message: err.Error(),
			Cause:   err,
		}
	}
	if err := c.waitForReceipt(ctx, tx.Hash()); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{
		Hash:   tx.Hash().Hex(),
		Target: target,
	}, nil
}

func (c *SafeTransactionClient) buildSignedSafeTransaction(ctx context.Context, target common.Address, data []byte, operation uint8) (*types.Transaction, error) {
	if c.safe == c.from {
		return c.buildSignedDirectTransaction(ctx, target, data)
	}
	execData, err := c.buildSafeExecData(ctx, target, data, operation)
	if err != nil {
		return nil, err
	}
	return c.buildSignedDirectTransaction(ctx, c.safe, execData)
}

func (c *SafeTransactionClient) buildSafeExecData(ctx context.Context, target common.Address, innerData []byte, operation uint8) ([]byte, error) {
	zero := big.NewInt(0)
	zeroAddr := common.Address{}

	nonceData, err := safeABI.Pack("nonce")
	if err != nil {
		return nil, requestError("ctf.safe_tx.build", err)
	}
	nonceRaw, err := c.backend.CallContract(ctx, ethereum.CallMsg{
		From: c.from,
		To:   &c.safe,
		Data: nonceData,
	}, nil)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.build",
			Message: err.Error(),
			Cause:   err,
		}
	}
	nonceOut, err := safeABI.Unpack("nonce", nonceRaw)
	if err != nil || len(nonceOut) != 1 {
		if err == nil {
			err = errMissing("unexpected safe nonce response")
		}
		return nil, requestError("ctf.safe_tx.build", err)
	}
	safeNonce, ok := nonceOut[0].(*big.Int)
	if !ok {
		return nil, requestError("ctf.safe_tx.build", errMissing("unexpected safe nonce type"))
	}

	hashData, err := safeABI.Pack(
		"getTransactionHash",
		target, zero, innerData,
		operation, zero, zero, zero,
		zeroAddr, zeroAddr, safeNonce,
	)
	if err != nil {
		return nil, requestError("ctf.safe_tx.build", err)
	}
	hashRaw, err := c.backend.CallContract(ctx, ethereum.CallMsg{
		From: c.from,
		To:   &c.safe,
		Data: hashData,
	}, nil)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.build",
			Message: err.Error(),
			Cause:   err,
		}
	}
	hashOut, err := safeABI.Unpack("getTransactionHash", hashRaw)
	if err != nil || len(hashOut) != 1 {
		if err == nil {
			err = errMissing("unexpected safe transaction hash response")
		}
		return nil, requestError("ctf.safe_tx.build", err)
	}
	txHash, ok := hashOut[0].([32]byte)
	if !ok {
		return nil, requestError("ctf.safe_tx.build", errMissing("unexpected safe transaction hash type"))
	}

	signature, err := crypto.Sign(txHash[:], c.privateKey)
	if err != nil {
		return nil, requestError("ctf.safe_tx.build", err)
	}
	if signature[64] < 27 {
		signature[64] += 27
	}

	execData, err := safeABI.Pack(
		"execTransaction",
		target, zero, innerData,
		operation, zero, zero, zero,
		zeroAddr, zeroAddr, signature,
	)
	if err != nil {
		return nil, requestError("ctf.safe_tx.build", err)
	}
	return execData, nil
}

func (c *SafeTransactionClient) buildSignedDirectTransaction(ctx context.Context, target common.Address, data []byte) (*types.Transaction, error) {
	nonce, err := c.backend.PendingNonceAt(ctx, c.from)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.build",
			Message: err.Error(),
			Cause:   err,
		}
	}
	gasPrice, err := c.backend.SuggestGasPrice(ctx)
	if err != nil {
		return nil, &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.build",
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
				Op:      "ctf.safe_tx.build",
				Message: err.Error(),
				Cause:   err,
			}
		}
		gasLimit = estimated * 120 / 100
	}
	if err := c.ensureNativeBalance(ctx, gasLimit, gasPrice); err != nil {
		return nil, err
	}

	tx := types.NewTransaction(nonce, target, big.NewInt(0), gasLimit, gasPrice, data)
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(c.chainID), c.privateKey)
	if err != nil {
		return nil, requestError("ctf.safe_tx.build", err)
	}
	return signed, nil
}

func (c *SafeTransactionClient) ensureNativeBalance(ctx context.Context, gasLimit uint64, gasPrice *big.Int) error {
	if gasPrice == nil {
		return requestError("ctf.safe_tx.build", errMissing("gas_price is required"))
	}
	balance, err := c.backend.BalanceAt(ctx, c.from, nil)
	if err != nil {
		return &polyerrors.Error{
			Kind:    classifyRPCError(err),
			Op:      "ctf.safe_tx.build",
			Message: err.Error(),
			Cause:   err,
		}
	}
	required := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), gasPrice)
	if balance.Cmp(required) >= 0 {
		return nil
	}
	shortfall := new(big.Int).Sub(required, balance)
	return requestError("ctf.safe_tx.build", fmt.Errorf("insufficient native token for gas: from=%s have=%s need=%s shortfall=%s gas_limit=%d gas_price=%s",
		c.from.Hex(), formatNativeToken(balance), formatNativeToken(required), formatNativeToken(shortfall), gasLimit, gasPrice.String()))
}

func (c *SafeTransactionClient) waitForReceipt(ctx context.Context, txHash common.Hash) error {
	for i := 0; i < c.receiptPollAttempts; i++ {
		receipt, err := c.backend.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.Status == types.ReceiptStatusSuccessful {
				return nil
			}
			return &polyerrors.Error{
				Kind:    polyerrors.ErrAPI,
				Op:      "ctf.safe_tx.receipt",
				Message: fmt.Sprintf("transaction reverted: tx=%s gas=%d", txHash.Hex(), receipt.GasUsed),
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if i == c.receiptPollAttempts-1 {
			break
		}
		timer := time.NewTimer(c.receiptPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return &polyerrors.Error{
		Kind:    polyerrors.ErrTimeout,
		Op:      "ctf.safe_tx.receipt",
		Message: "transaction receipt timeout: " + txHash.Hex(),
	}
}

func formatNativeToken(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return new(big.Rat).SetFrac(value, big.NewInt(1_000_000_000_000_000_000)).FloatString(6)
}
