package auth

import "testing"

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
	if order.Signature == "" {
		t.Fatal("expected signature")
	}
}
