package auth

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	CTFExchangeAddress        = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	NegRiskCTFExchangeAddress = "0xC5d563A36AE78145C45a50134d48A1215220f80a"
)

var (
	domainTypeHash = crypto.Keccak256Hash([]byte(
		"EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	orderTypeHash = crypto.Keccak256Hash([]byte(
		"Order(uint256 salt,address maker,address signer,address taker,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint256 expiration,uint256 nonce,uint256 feeRateBps,uint8 side,uint8 signatureType)"))
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
	Taker         string `json:"taker"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Expiration    string `json:"expiration"`
	Nonce         int    `json:"nonce"`
	FeeRateBps    int    `json:"feeRateBps"`
	Side          Side   `json:"side"`
	SignatureType int    `json:"signatureType"`
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
	FeeRateBPS int
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
	if s.signatureType == 2 && strings.TrimSpace(s.funder) != "" {
		return strings.ToLower(s.funder)
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
	message := ts + args.Method + args.RequestPath + canonicalizeBody(args.Body)

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

	saltVal := int64(float64(time.Now().Unix()) * rand.Float64())
	saltBig := new(big.Int).SetInt64(saltVal)

	tokenIDBig := new(big.Int)
	if _, ok := tokenIDBig.SetString(req.TokenID, 10); !ok {
		if _, ok := tokenIDBig.SetString(strings.TrimPrefix(req.TokenID, "0x"), 16); !ok {
			return SignedOrder{}, fmt.Errorf("invalid token id: %s", req.TokenID)
		}
	}

	signerAddr := s.address
	taker := common.Address{}
	expirationBig := new(big.Int).SetInt64(req.Expiration)
	nonce := big.NewInt(0)
	feeRateBps := new(big.Int).SetInt64(int64(req.FeeRateBPS))

	maker := s.address
	if s.signatureType == 2 && strings.TrimSpace(s.funder) != "" {
		maker = common.HexToAddress(s.funder)
	}

	exchangeAddr := CTFExchangeAddress
	if req.NegRisk {
		exchangeAddr = NegRiskCTFExchangeAddress
	}

	structHash := crypto.Keccak256Hash(
		orderTypeHash.Bytes(),
		common.LeftPadBytes(saltBig.Bytes(), 32),
		common.LeftPadBytes(maker.Bytes(), 32),
		common.LeftPadBytes(signerAddr.Bytes(), 32),
		common.LeftPadBytes(taker.Bytes(), 32),
		common.LeftPadBytes(tokenIDBig.Bytes(), 32),
		common.LeftPadBytes(makerAmount.Bytes(), 32),
		common.LeftPadBytes(takerAmount.Bytes(), 32),
		common.LeftPadBytes(expirationBig.Bytes(), 32),
		common.LeftPadBytes(nonce.Bytes(), 32),
		common.LeftPadBytes(feeRateBps.Bytes(), 32),
		common.LeftPadBytes(big.NewInt(sideInt).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(int64(s.signatureType)).Bytes(), 32),
	)

	domainSep := buildDomainSeparator(s.chainID, exchangeAddr)
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSep.Bytes(), structHash.Bytes())
	sig, err := crypto.Sign(digest.Bytes(), s.privateKey)
	if err != nil {
		return SignedOrder{}, err
	}
	if sig[64] < 27 {
		sig[64] += 27
	}

	return SignedOrder{
		Salt:          saltVal,
		Maker:         strings.ToLower(maker.Hex()),
		Signer:        strings.ToLower(signerAddr.Hex()),
		Taker:         strings.ToLower(taker.Hex()),
		TokenID:       req.TokenID,
		MakerAmount:   makerAmount.String(),
		TakerAmount:   takerAmount.String(),
		Expiration:    strconv.FormatInt(req.Expiration, 10),
		Nonce:         0,
		FeeRateBps:    req.FeeRateBPS,
		Side:          req.Side,
		SignatureType: s.signatureType,
		Signature:     "0x" + common.Bytes2Hex(sig),
	}, nil
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
	version := hashString("1")
	chainIDBig := new(big.Int).SetInt64(int64(chainID))

	return crypto.Keccak256Hash(
		domainTypeHash.Bytes(),
		name.Bytes(),
		version.Bytes(),
		common.LeftPadBytes(chainIDBig.Bytes(), 32),
		common.LeftPadBytes(common.HexToAddress(exchangeAddr).Bytes(), 32),
	)
}
