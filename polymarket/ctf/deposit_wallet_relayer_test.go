package ctf

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drinkthere/polymarket-sdk/polymarket/httpx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestDepositWalletRelayerMergePositionsSubmitsWalletBatchWithCTFCollateral(t *testing.T) {
	privateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	key, err := crypto.HexToECDSA(privateKey[2:])
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	wallet := common.HexToAddress("0x1690833999bf53eb8e2885a5e9a96280f7a7d790")

	var submitted depositWalletBatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != relayerUserAgent {
			t.Fatalf("user-agent = %q want %q", got, relayerUserAgent)
		}
		switch r.URL.Path {
		case "/nonce":
			if r.Method != http.MethodGet {
				t.Fatalf("nonce method = %s", r.Method)
			}
			if got := r.URL.Query().Get("address"); got != from.Hex() {
				t.Fatalf("nonce address = %q want %q", got, from.Hex())
			}
			if got := r.URL.Query().Get("type"); got != "WALLET" {
				t.Fatalf("nonce type = %q", got)
			}
			_, _ = w.Write([]byte(`{"nonce":"2"}`))
		case "/submit":
			if r.Method != http.MethodPost {
				t.Fatalf("submit method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatalf("decode submit: %v", err)
			}
			_, _ = w.Write([]byte(`{"transactionID":"tx-1","state":"STATE_NEW","transactionHash":"0xabc"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := NewDepositWalletRelayerClientWithHTTPClient(context.Background(), httpClient, DepositWalletRelayerConfig{
		PrivateKey:            privateKey,
		WalletAddress:         wallet.Hex(),
		ChainID:               137,
		DeadlineWindowSeconds: 240,
	})
	if err != nil {
		t.Fatalf("NewDepositWalletRelayerClientWithHTTPClient: %v", err)
	}

	result, err := client.MergePositions(context.Background(), MergePositionsRequest{
		ConditionID: validConditionID,
		IndexSets:   []uint64{1, 2},
		Amount:      big.NewInt(2_000_000),
	})
	if err != nil {
		t.Fatalf("MergePositions: %v", err)
	}
	if result.Hash != "0xabc" {
		t.Fatalf("hash = %q", result.Hash)
	}
	if submitted.Type != "WALLET" || submitted.From != from.Hex() || submitted.To != DepositWalletFactory.Hex() || submitted.Nonce != "2" {
		t.Fatalf("unexpected submit envelope: %+v", submitted)
	}
	if submitted.Signature == "" || submitted.Signature == "0x" {
		t.Fatalf("missing signature: %+v", submitted)
	}
	params := submitted.DepositWalletParams
	if params.DepositWallet != wallet.Hex() || len(params.Calls) != 1 {
		t.Fatalf("unexpected wallet params: %+v", params)
	}
	call := params.Calls[0]
	if call.Target != CTFContractAddress.Hex() || call.Value != "0" {
		t.Fatalf("unexpected call target/value: %+v", call)
	}
	data, err := hex.DecodeString(call.Data[2:])
	if err != nil {
		t.Fatalf("decode call data: %v", err)
	}
	method, err := mergeABI.MethodById(data[:4])
	if err != nil {
		t.Fatalf("merge method: %v", err)
	}
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack merge: %v", err)
	}
	if got := args[0].(common.Address); got != CTFCollateralAddress {
		t.Fatalf("merge collateral = %s want %s", got.Hex(), CTFCollateralAddress.Hex())
	}
}

func TestDepositWalletRelayerMergePositionsSendsRelayerAPIKeyHeaders(t *testing.T) {
	privateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	key, err := crypto.HexToECDSA(privateKey[2:])
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	wallet := common.HexToAddress("0x1690833999bf53eb8e2885a5e9a96280f7a7d790")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nonce":
			if got := r.Header.Get("RELAYER_API_KEY"); got != "relayer-key-1" {
				t.Fatalf("nonce relayer api key = %q", got)
			}
			if got := r.Header.Get("RELAYER_API_KEY_ADDRESS"); got != from.Hex() {
				t.Fatalf("nonce relayer api key address = %q want %q", got, from.Hex())
			}
			_, _ = w.Write([]byte(`{"nonce":"2"}`))
		case "/submit":
			if got := r.Header.Get("RELAYER_API_KEY"); got != "relayer-key-1" {
				t.Fatalf("submit relayer api key = %q", got)
			}
			if got := r.Header.Get("RELAYER_API_KEY_ADDRESS"); got != from.Hex() {
				t.Fatalf("submit relayer api key address = %q want %q", got, from.Hex())
			}
			_, _ = w.Write([]byte(`{"transactionHash":"0xabc"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := NewDepositWalletRelayerClientWithHTTPClient(context.Background(), httpClient, DepositWalletRelayerConfig{
		PrivateKey:    privateKey,
		WalletAddress: wallet.Hex(),
		ChainID:       137,
		RelayerCredentials: RelayerCredentials{
			Key:        "relayer-key-1",
			KeyAddress: from.Hex(),
		},
	})
	if err != nil {
		t.Fatalf("NewDepositWalletRelayerClientWithHTTPClient: %v", err)
	}

	if _, err := client.MergePositions(context.Background(), MergePositionsRequest{
		ConditionID: validConditionID,
		IndexSets:   []uint64{1, 2},
		Amount:      big.NewInt(2_000_000),
	}); err != nil {
		t.Fatalf("MergePositions: %v", err)
	}
}

func TestDepositWalletRelayerMergePositionsAndWrapUSDCeSubmitsSingleBatch(t *testing.T) {
	privateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	key, err := crypto.HexToECDSA(privateKey[2:])
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	wallet := common.HexToAddress("0x1690833999bf53eb8e2885a5e9a96280f7a7d790")

	var submitted depositWalletBatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nonce":
			_, _ = w.Write([]byte(`{"nonce":"2"}`))
		case "/submit":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatalf("decode submit: %v", err)
			}
			if submitted.From != from.Hex() {
				t.Fatalf("from = %s want %s", submitted.From, from.Hex())
			}
			_, _ = w.Write([]byte(`{"transactionHash":"0xabc"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := NewDepositWalletRelayerClientWithHTTPClient(context.Background(), httpClient, DepositWalletRelayerConfig{
		PrivateKey:    privateKey,
		WalletAddress: wallet.Hex(),
		ChainID:       137,
	})
	if err != nil {
		t.Fatalf("NewDepositWalletRelayerClientWithHTTPClient: %v", err)
	}

	amount := big.NewInt(2_000_000)
	if _, err := client.MergePositionsAndWrapUSDCe(context.Background(), MergePositionsRequest{
		ConditionID: validConditionID,
		IndexSets:   []uint64{1, 2},
		Amount:      amount,
	}); err != nil {
		t.Fatalf("MergePositionsAndWrapUSDCe: %v", err)
	}

	calls := submitted.DepositWalletParams.Calls
	if len(calls) != 3 {
		t.Fatalf("calls len = %d want 3: %+v", len(calls), calls)
	}
	if calls[0].Target != CTFContractAddress.Hex() {
		t.Fatalf("merge target = %s", calls[0].Target)
	}
	if calls[1].Target != USDCeAddress.Hex() {
		t.Fatalf("approve target = %s want %s", calls[1].Target, USDCeAddress.Hex())
	}
	if calls[2].Target != CollateralOnrampAddress.Hex() {
		t.Fatalf("wrap target = %s want %s", calls[2].Target, CollateralOnrampAddress.Hex())
	}

	mergeData, err := hex.DecodeString(calls[0].Data[2:])
	if err != nil {
		t.Fatalf("decode merge data: %v", err)
	}
	mergeMethod, err := mergeABI.MethodById(mergeData[:4])
	if err != nil {
		t.Fatalf("merge method: %v", err)
	}
	mergeArgs, err := mergeMethod.Inputs.Unpack(mergeData[4:])
	if err != nil {
		t.Fatalf("unpack merge: %v", err)
	}
	if got := mergeArgs[0].(common.Address); got != USDCeAddress {
		t.Fatalf("merge collateral = %s want %s", got.Hex(), USDCeAddress.Hex())
	}

	approveData, err := hex.DecodeString(calls[1].Data[2:])
	if err != nil {
		t.Fatalf("decode approve data: %v", err)
	}
	approveMethod, err := erc20ApproveABI.MethodById(approveData[:4])
	if err != nil {
		t.Fatalf("approve method: %v", err)
	}
	approveArgs, err := approveMethod.Inputs.Unpack(approveData[4:])
	if err != nil {
		t.Fatalf("unpack approve: %v", err)
	}
	if got := approveArgs[0].(common.Address); got != CollateralOnrampAddress {
		t.Fatalf("approve spender = %s want %s", got.Hex(), CollateralOnrampAddress.Hex())
	}
	if got := approveArgs[1].(*big.Int); got.Cmp(amount) != 0 {
		t.Fatalf("approve amount = %s want %s", got, amount)
	}

	wrapData, err := hex.DecodeString(calls[2].Data[2:])
	if err != nil {
		t.Fatalf("decode wrap data: %v", err)
	}
	wrapMethod, err := collateralOnrampABI.MethodById(wrapData[:4])
	if err != nil {
		t.Fatalf("wrap method: %v", err)
	}
	wrapArgs, err := wrapMethod.Inputs.Unpack(wrapData[4:])
	if err != nil {
		t.Fatalf("unpack wrap: %v", err)
	}
	if got := wrapArgs[0].(common.Address); got != USDCeAddress {
		t.Fatalf("wrap asset = %s want %s", got.Hex(), USDCeAddress.Hex())
	}
	if got := wrapArgs[1].(common.Address); got != wallet {
		t.Fatalf("wrap recipient = %s want %s", got.Hex(), wallet.Hex())
	}
	if got := wrapArgs[2].(*big.Int); got.Cmp(amount) != 0 {
		t.Fatalf("wrap amount = %s want %s", got, amount)
	}
}

func TestDepositWalletRelayerRedeemPositionsSubmitsWalletBatch(t *testing.T) {
	privateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	wallet := common.HexToAddress("0x1690833999bf53eb8e2885a5e9a96280f7a7d790")

	var submitted depositWalletBatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nonce":
			_, _ = w.Write([]byte(`{"nonce":"3"}`))
		case "/submit":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatalf("decode submit: %v", err)
			}
			_, _ = w.Write([]byte(`{"transactionHash":"0xdef"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := NewDepositWalletRelayerClientWithHTTPClient(context.Background(), httpClient, DepositWalletRelayerConfig{
		PrivateKey:    privateKey,
		WalletAddress: wallet.Hex(),
		ChainID:       137,
	})
	if err != nil {
		t.Fatalf("NewDepositWalletRelayerClientWithHTTPClient: %v", err)
	}

	req := RedeemPositionsRequest{
		ConditionID: validConditionID,
		IndexSets:   []uint64{1, 2},
	}
	target, data, err := BuildRedeemPositionsCalldata(req)
	if err != nil {
		t.Fatalf("BuildRedeemPositionsCalldata: %v", err)
	}
	result, err := client.RedeemPositions(context.Background(), req)
	if err != nil {
		t.Fatalf("RedeemPositions: %v", err)
	}
	if result.Hash != "0xdef" || result.Target != target {
		t.Fatalf("unexpected result: %+v", result)
	}
	calls := submitted.DepositWalletParams.Calls
	if len(calls) != 1 {
		t.Fatalf("calls len = %d want 1: %+v", len(calls), calls)
	}
	if calls[0].Target != target.Hex() || calls[0].Value != "0" || calls[0].Data != "0x"+hex.EncodeToString(data) {
		t.Fatalf("unexpected redeem call: %+v", calls[0])
	}
}

func TestDepositWalletRelayerNegRiskRedeemSubmitsWalletBatch(t *testing.T) {
	privateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	wallet := common.HexToAddress("0x1690833999bf53eb8e2885a5e9a96280f7a7d790")

	var submitted depositWalletBatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nonce":
			_, _ = w.Write([]byte(`{"nonce":"4"}`))
		case "/submit":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatalf("decode submit: %v", err)
			}
			_, _ = w.Write([]byte(`{"transactionHash":"0x456"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	httpClient, err := httpx.New(httpx.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	client, err := NewDepositWalletRelayerClientWithHTTPClient(context.Background(), httpClient, DepositWalletRelayerConfig{
		PrivateKey:    privateKey,
		WalletAddress: wallet.Hex(),
		ChainID:       137,
	})
	if err != nil {
		t.Fatalf("NewDepositWalletRelayerClientWithHTTPClient: %v", err)
	}

	req := NegRiskRedeemRequest{
		ConditionID: validConditionID,
		Amounts:     []*big.Int{big.NewInt(1_000_000), big.NewInt(0)},
	}
	target, data, err := BuildNegRiskRedeemCalldata(req)
	if err != nil {
		t.Fatalf("BuildNegRiskRedeemCalldata: %v", err)
	}
	result, err := client.NegRiskRedeem(context.Background(), req)
	if err != nil {
		t.Fatalf("NegRiskRedeem: %v", err)
	}
	if result.Hash != "0x456" || result.Target != target {
		t.Fatalf("unexpected result: %+v", result)
	}
	calls := submitted.DepositWalletParams.Calls
	if len(calls) != 1 {
		t.Fatalf("calls len = %d want 1: %+v", len(calls), calls)
	}
	if calls[0].Target != target.Hex() || calls[0].Value != "0" || calls[0].Data != "0x"+hex.EncodeToString(data) {
		t.Fatalf("unexpected neg-risk redeem call: %+v", calls[0])
	}
}
