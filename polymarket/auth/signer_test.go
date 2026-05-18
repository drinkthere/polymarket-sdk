package auth

import (
	"strings"
	"testing"
)

func TestSignerCreateSignedOrderUsesFunderAsMakerForSignatureType2(t *testing.T) {
	t.Parallel()

	signer, err := NewSigner(Config{
		FunderAddress: "0x1111111111111111111111111111111111111111",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       137,
		SignatureType: 2,
	})
	if err != nil {
		t.Fatalf("NewSigner() error: %v", err)
	}

	order, err := signer.CreateSignedOrder(CreateSignedOrderRequest{
		TokenID:    "123456789",
		Price:      0.42,
		Size:       5,
		Side:       SideBuy,
		NegRisk:    false,
		TickSize:   0.01,
		Expiration: 12345,
		Timestamp:  1713398400000,
	})
	if err != nil {
		t.Fatalf("CreateSignedOrder() error: %v", err)
	}
	if order.Maker != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected maker: %q", order.Maker)
	}
	if order.Signer != "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266" {
		t.Fatalf("unexpected signer: %q", order.Signer)
	}
	if order.Side != SideBuy {
		t.Fatalf("unexpected side: %q", order.Side)
	}
	if order.MakerAmount != "2100000" {
		t.Fatalf("unexpected maker amount: %q", order.MakerAmount)
	}
	if order.TakerAmount != "5000000" {
		t.Fatalf("unexpected taker amount: %q", order.TakerAmount)
	}
	if order.SignatureType != 2 {
		t.Fatalf("unexpected signature type: %d", order.SignatureType)
	}
	if order.Timestamp != "1713398400000" {
		t.Fatalf("unexpected timestamp: %q", order.Timestamp)
	}
	if order.Metadata != Bytes32Zero {
		t.Fatalf("unexpected metadata: %q", order.Metadata)
	}
	if order.Builder != Bytes32Zero {
		t.Fatalf("unexpected builder: %q", order.Builder)
	}
	if order.Signature == "" {
		t.Fatal("expected signature")
	}
}

func TestSignerCreateSignedOrderUsesFunderAsMakerAndSignerForSignatureType3(t *testing.T) {
	t.Parallel()

	signer, err := NewSigner(Config{
		FunderAddress: "0x1111111111111111111111111111111111111111",
		PrivateKey:    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		ChainID:       137,
		SignatureType: 3,
	})
	if err != nil {
		t.Fatalf("NewSigner() error: %v", err)
	}

	order, err := signer.CreateSignedOrder(CreateSignedOrderRequest{
		TokenID:    "123456789",
		Price:      0.42,
		Size:       5,
		Side:       SideBuy,
		NegRisk:    false,
		TickSize:   0.01,
		Expiration: 12345,
		Timestamp:  1713398400000,
		Salt:       987654321,
	})
	if err != nil {
		t.Fatalf("CreateSignedOrder() error: %v", err)
	}
	if order.Maker != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected maker: %q", order.Maker)
	}
	if order.Signer != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected signer: %q", order.Signer)
	}
	if order.SignatureType != 3 {
		t.Fatalf("unexpected signature type: %d", order.SignatureType)
	}
	if order.Salt != 987654321 {
		t.Fatalf("unexpected salt: %d", order.Salt)
	}
	const wantSignature = "0x253a9d945a1e9d44b0bda619f4dd47afc4e34063b3a1c58c1d91de9131dda40e20e961f0b4d09305256c0b4adba8d267d6e2baacb52fc8a67aacc1a6dfe076e31c3264e159346253e26a64e00b69032db0e7d32f94628de3e6eecb50304d7af3d25592f454c5174b214c36cfa28ffa091b81e96ef6164ed3895c872b0184ba92514f726465722875696e743235362073616c742c61646472657373206d616b65722c61646472657373207369676e65722c75696e7432353620746f6b656e49642c75696e74323536206d616b6572416d6f756e742c75696e743235362074616b6572416d6f756e742c75696e743820736964652c75696e7438207369676e6174757265547970652c75696e743235362074696d657374616d702c62797465733332206d657461646174612c62797465733332206275696c6465722900ba"
	if order.Signature != wantSignature {
		t.Fatalf("unexpected signature:\ngot  %s\nwant %s", order.Signature, wantSignature)
	}
	if !strings.HasSuffix(order.Signature, "2900ba") {
		t.Fatalf("expected ERC-7739 contents type length suffix, got %q", order.Signature)
	}
}
