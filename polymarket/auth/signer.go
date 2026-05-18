package auth

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	CTFExchangeV2Address        = "0xE111180000d2663C0091e4f400237545B87B996B"
	NegRiskCTFExchangeV2Address = "0xe2222d279d744050d28e00520010520000310F59"
	Bytes32Zero                 = "0x0000000000000000000000000000000000000000000000000000000000000000"
	SignatureTypePoly1271       = 3
)

var (
	orderTypeString = "Order(uint256 salt,address maker,address signer,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side,uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"
	domainTypeHash  = crypto.Keccak256Hash([]byte(
		"EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	orderTypeHash  = crypto.Keccak256Hash([]byte(orderTypeString))
	soladyTypeHash = crypto.Keccak256Hash([]byte(
		"TypedDataSign(Order contents,string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)" + orderTypeString))
	depositWalletNameHash    = hashString("DepositWallet")
	depositWalletVersionHash = hashString("1")
)

type APICredentials struct {
	Key        string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

func (c APICredentials) Valid() bool {
	return strings.TrimSpace(c.Key) != "" &&
		strings.TrimSpace(c.Secret) != "" &&
		strings.TrimSpace(c.Passphrase) != ""
}

func (c APICredentials) ToCredentials(address, funder string) Credentials {
	return Credentials{
		APICredentials: c,
		Address:        address,
		FunderAddress:  funder,
	}
}

type Credentials struct {
	APICredentials
	Address       string
	FunderAddress string
}

func (c Credentials) Valid() bool {
	return c.APICredentials.Valid()
}

type L1PolyHeader struct {
	POLY_ADDRESS   string
	POLY_SIGNATURE string
	POLY_TIMESTAMP string
	POLY_NONCE     string
}

type L2HeaderArgs struct {
	Method      string
	RequestPath string
	Body        string
}

type L2PolyHeader struct {
	POLY_ADDRESS    string
	POLY_SIGNATURE  string
	POLY_TIMESTAMP  string
	POLY_API_KEY    string
	POLY_PASSPHRASE string
}

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type SignedOrder struct {
	Salt          int64  `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Expiration    string `json:"expiration"`
	Side          Side   `json:"side"`
	SignatureType int    `json:"signatureType"`
	Timestamp     string `json:"timestamp"`
	Metadata      string `json:"metadata"`
	Builder       string `json:"builder"`
	Signature     string `json:"signature"`
}

type CreateSignedOrderRequest struct {
	TokenID    string
	Price      float64
	Size       float64
	Side       Side
	NegRisk    bool
	TickSize   float64
	Expiration int64
	Timestamp  int64
	Salt       int64
	Metadata   string
	Builder    string
}

type Signer struct {
	privateKey    *ecdsa.PrivateKey
	address       common.Address
	chainID       int
	signatureType int
	funder        string
}

func NewSigner(cfg Config) (*Signer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	privateKeyHex := strings.TrimSpace(cfg.PrivateKey)
	if strings.HasPrefix(privateKeyHex, "0x") {
		privateKeyHex = privateKeyHex[2:]
	}
	pk, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	chainID := cfg.ChainID
	if chainID == 0 {
		chainID = 137
	}
	signatureType := cfg.SignatureType
	if signatureType == 0 {
		signatureType = 2
	}

	return &Signer{
		privateKey:    pk,
		address:       crypto.PubkeyToAddress(pk.PublicKey),
		chainID:       chainID,
		signatureType: signatureType,
		funder:        cfg.FunderAddress,
	}, nil
}

func (s *Signer) Address() string {
	if s == nil {
		return ""
	}
	return strings.ToLower(s.address.Hex())
}

func (s *Signer) CredentialsFromConfig(cfg Config) Credentials {
	return Credentials{
		APICredentials: cfg.APICredentials(),
		Address:        s.Address(),
		FunderAddress:  strings.ToLower(cfg.FunderAddress),
	}
}

func (s *Signer) CreateL1Headers(at time.Time, nonce int64) (L1PolyHeader, error) {
	sig, err := s.signClobAuth(at.Unix(), nonce)
	if err != nil {
		return L1PolyHeader{}, err
	}
	return L1PolyHeader{
		POLY_ADDRESS:   s.Address(),
		POLY_SIGNATURE: sig,
		POLY_TIMESTAMP: strconv.FormatInt(at.Unix(), 10),
		POLY_NONCE:     strconv.FormatInt(nonce, 10),
	}, nil
}

func (s *Signer) CreateL2Headers(creds APICredentials, args L2HeaderArgs, at time.Time) (L2PolyHeader, error) {
	if s == nil {
		return L2PolyHeader{}, fmt.Errorf("signer is required")
	}
	if !creds.Valid() {
		return L2PolyHeader{}, fmt.Errorf("api credentials are required")
	}

	secretBytes, err := decodeSecret(creds.Secret)
	if err != nil {
		return L2PolyHeader{}, err
	}

	ts := strconv.FormatInt(at.Unix(), 10)
	message := ts + args.Method + args.RequestPath + args.Body

	mac := hmac.New(sha256.New, secretBytes)
	_, _ = mac.Write([]byte(message))

	return L2PolyHeader{
		POLY_ADDRESS:    s.Address(),
		POLY_SIGNATURE:  base64.URLEncoding.EncodeToString(mac.Sum(nil)),
		POLY_TIMESTAMP:  ts,
		POLY_API_KEY:    creds.Key,
		POLY_PASSPHRASE: creds.Passphrase,
	}, nil
}

func (s *Signer) AddL2Headers(headers map[string]string, creds APICredentials, method, path string, body []byte) error {
	l2, err := s.CreateL2Headers(creds, L2HeaderArgs{
		Method:      method,
		RequestPath: path,
		Body:        string(body),
	}, time.Now())
	if err != nil {
		return err
	}
	headers["POLY_ADDRESS"] = l2.POLY_ADDRESS
	headers["POLY_API_KEY"] = l2.POLY_API_KEY
	headers["POLY_SIGNATURE"] = l2.POLY_SIGNATURE
	headers["POLY_TIMESTAMP"] = l2.POLY_TIMESTAMP
	headers["POLY_PASSPHRASE"] = l2.POLY_PASSPHRASE
	return nil
}

func (s *Signer) CreateSignedOrder(req CreateSignedOrderRequest) (SignedOrder, error) {
	if s == nil || s.privateKey == nil {
		return SignedOrder{}, fmt.Errorf("no private key configured")
	}
	if req.TickSize <= 0 {
		return SignedOrder{}, fmt.Errorf("tick_size must be > 0")
	}

	decimals := int(math.Round(-math.Log10(req.TickSize)))
	priceStr := strconv.FormatFloat(req.Price, 'f', decimals, 64)
	priceF, _ := strconv.ParseFloat(priceStr, 64)

	var makerAmount, takerAmount *big.Int
	sideInt := int64(0)
	if req.Side == SideSell {
		sideInt = 1
	}

	if req.Side == SideBuy {
		makerFloat := priceF * req.Size * 1e6
		makerAmount = new(big.Int).SetUint64(uint64(math.Round(makerFloat)))
		takerAmount = new(big.Int).SetUint64(uint64(math.Round(req.Size * 1e6)))
	} else {
		makerAmount = new(big.Int).SetUint64(uint64(math.Round(req.Size * 1e6)))
		takerFloat := priceF * req.Size * 1e6
		takerAmount = new(big.Int).SetUint64(uint64(math.Round(takerFloat)))
	}

	saltVal := req.Salt
	if saltVal <= 0 {
		saltVal = int64(float64(time.Now().Unix()) * rand.Float64())
	}
	saltBig := new(big.Int).SetInt64(saltVal)

	tokenIDBig := new(big.Int)
	if _, ok := tokenIDBig.SetString(req.TokenID, 10); !ok {
		if _, ok := tokenIDBig.SetString(strings.TrimPrefix(req.TokenID, "0x"), 16); !ok {
			return SignedOrder{}, fmt.Errorf("invalid token id: %s", req.TokenID)
		}
	}

	signerAddr := s.address

	maker := s.address
	if s.signatureType == 2 && strings.TrimSpace(s.funder) != "" {
		maker = common.HexToAddress(s.funder)
	}
	if s.signatureType == SignatureTypePoly1271 {
		if strings.TrimSpace(s.funder) == "" {
			return SignedOrder{}, fmt.Errorf("funder_address is required for signature_type=3")
		}
		maker = common.HexToAddress(s.funder)
		signerAddr = maker
	}

	exchangeAddr := CTFExchangeV2Address
	if req.NegRisk {
		exchangeAddr = NegRiskCTFExchangeV2Address
	}

	timestamp := req.Timestamp
	if timestamp <= 0 {
		timestamp = time.Now().UnixMilli()
	}
	timestampBig := new(big.Int).SetInt64(timestamp)

	metadata := strings.TrimSpace(req.Metadata)
	if metadata == "" {
		metadata = Bytes32Zero
	}
	builder := strings.TrimSpace(req.Builder)
	if builder == "" {
		builder = Bytes32Zero
	}

	structHash := crypto.Keccak256Hash(
		orderTypeHash.Bytes(),
		common.LeftPadBytes(saltBig.Bytes(), 32),
		common.LeftPadBytes(maker.Bytes(), 32),
		common.LeftPadBytes(signerAddr.Bytes(), 32),
		common.LeftPadBytes(tokenIDBig.Bytes(), 32),
		common.LeftPadBytes(makerAmount.Bytes(), 32),
		common.LeftPadBytes(takerAmount.Bytes(), 32),
		common.LeftPadBytes(big.NewInt(sideInt).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(int64(s.signatureType)).Bytes(), 32),
		common.LeftPadBytes(timestampBig.Bytes(), 32),
		hexToBytes32(metadata),
		hexToBytes32(builder),
	)

	domainSep := buildDomainSeparator(s.chainID, exchangeAddr)
	sig, err := s.signOrder(domainSep, structHash, signerAddr)
	if err != nil {
		return SignedOrder{}, err
	}

	return SignedOrder{
		Salt:          saltVal,
		Maker:         strings.ToLower(maker.Hex()),
		Signer:        strings.ToLower(signerAddr.Hex()),
		TokenID:       req.TokenID,
		MakerAmount:   makerAmount.String(),
		TakerAmount:   takerAmount.String(),
		Expiration:    strconv.FormatInt(req.Expiration, 10),
		Side:          req.Side,
		SignatureType: s.signatureType,
		Timestamp:     strconv.FormatInt(timestamp, 10),
		Metadata:      metadata,
		Builder:       builder,
		Signature:     "0x" + common.Bytes2Hex(sig),
	}, nil
}

func (s *Signer) signOrder(domainSep common.Hash, contentsHash common.Hash, signerAddr common.Address) ([]byte, error) {
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSep.Bytes(), contentsHash.Bytes())
	if s.signatureType == SignatureTypePoly1271 {
		typedDataSignStructHash := crypto.Keccak256Hash(
			soladyTypeHash.Bytes(),
			contentsHash.Bytes(),
			depositWalletNameHash.Bytes(),
			depositWalletVersionHash.Bytes(),
			common.LeftPadBytes(big.NewInt(int64(s.chainID)).Bytes(), 32),
			common.LeftPadBytes(signerAddr.Bytes(), 32),
			common.Hex2Bytes(strings.TrimPrefix(Bytes32Zero, "0x")),
		)
		digest = crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSep.Bytes(), typedDataSignStructHash.Bytes())
	}

	sig, err := crypto.Sign(digest.Bytes(), s.privateKey)
	if err != nil {
		return nil, err
	}
	if sig[64] < 27 {
		sig[64] += 27
	}
	if s.signatureType != SignatureTypePoly1271 {
		return sig, nil
	}
	return appendPoly1271Context(sig, domainSep, contentsHash), nil
}

func appendPoly1271Context(sig []byte, domainSep common.Hash, contentsHash common.Hash) []byte {
	out := make([]byte, 0, len(sig)+32+32+len(orderTypeString)+2)
	out = append(out, sig...)
	out = append(out, domainSep.Bytes()...)
	out = append(out, contentsHash.Bytes()...)
	out = append(out, []byte(orderTypeString)...)
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(orderTypeString)))
	out = append(out, length[:]...)
	return out
}

func (s *Signer) signClobAuth(timestamp int64, nonce int64) (string, error) {
	if s == nil || s.privateKey == nil {
		return "", fmt.Errorf("no private key configured")
	}

	clobAuthTypeHash := crypto.Keccak256Hash([]byte(
		"ClobAuth(address address,string timestamp,uint256 nonce,string message)"))
	clobDomainTypeHash := crypto.Keccak256Hash([]byte(
		"EIP712Domain(string name,string version,uint256 chainId)"))
	domainSep := crypto.Keccak256Hash(
		clobDomainTypeHash.Bytes(),
		hashString("ClobAuthDomain").Bytes(),
		hashString("1").Bytes(),
		common.LeftPadBytes(new(big.Int).SetInt64(int64(s.chainID)).Bytes(), 32),
	)

	timestampStr := strconv.FormatInt(timestamp, 10)
	message := "This message attests that I control the given wallet"
	structHash := crypto.Keccak256Hash(
		clobAuthTypeHash.Bytes(),
		common.LeftPadBytes(s.address.Bytes(), 32),
		hashString(timestampStr).Bytes(),
		common.LeftPadBytes(new(big.Int).SetInt64(nonce).Bytes(), 32),
		hashString(message).Bytes(),
	)

	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSep.Bytes(), structHash.Bytes())
	sig, err := crypto.Sign(digest.Bytes(), s.privateKey)
	if err != nil {
		return "", err
	}
	if sig[64] < 27 {
		sig[64] += 27
	}
	return "0x" + common.Bytes2Hex(sig), nil
}

func decodeSecret(secret string) ([]byte, error) {
	decoded, err := base64.URLEncoding.DecodeString(secret)
	if err == nil {
		return decoded, nil
	}
	decoded, err = base64.StdEncoding.DecodeString(secret)
	if err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("decode api secret: %w", err)
}

func canonicalizeBody(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ""
	}

	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return body
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return string(encoded)
}

func hashString(s string) common.Hash {
	return crypto.Keccak256Hash([]byte(s))
}

func buildDomainSeparator(chainID int, exchangeAddr string) common.Hash {
	name := hashString("Polymarket CTF Exchange")
	version := hashString("2")
	chainIDBig := new(big.Int).SetInt64(int64(chainID))

	return crypto.Keccak256Hash(
		domainTypeHash.Bytes(),
		name.Bytes(),
		version.Bytes(),
		common.LeftPadBytes(chainIDBig.Bytes(), 32),
		common.LeftPadBytes(common.HexToAddress(exchangeAddr).Bytes(), 32),
	)
}

func hexToBytes32(value string) []byte {
	raw := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(raw) < 64 {
		raw = strings.Repeat("0", 64-len(raw)) + raw
	}
	if len(raw) > 64 {
		raw = raw[len(raw)-64:]
	}
	return common.Hex2Bytes(raw)
}
