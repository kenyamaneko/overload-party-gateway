package platform

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	appleProductionURL = "https://api.storekit.itunes.apple.com"
	appleSandboxURL    = "https://api.storekit-sandbox.itunes.apple.com"
	appleAPITimeout    = 10 * time.Second
)

// AppleReceiptVerifier implements ReceiptVerifier using App Store Server API v2.
type AppleReceiptVerifier struct {
	keyID      string
	issuerID   string
	bundleID   string
	privateKey *ecdsa.PrivateKey
	baseURL    string
	httpClient *http.Client
}

// NewAppleReceiptVerifier creates a new Apple receipt verifier.
// privateKeyPath is the path to a PEM-encoded P-256 private key from App Store Connect.
// environment should be "Production" or "Sandbox".
func NewAppleReceiptVerifier(keyID, issuerID, bundleID, privateKeyPath, environment string) (*AppleReceiptVerifier, error) {
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read apple private key: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ECDSA")
	}

	baseURL := appleProductionURL
	if environment == "Sandbox" {
		baseURL = appleSandboxURL
	}

	return &AppleReceiptVerifier{
		keyID:      keyID,
		issuerID:   issuerID,
		bundleID:   bundleID,
		privateKey: ecKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: appleAPITimeout},
	}, nil
}

// Compile-time interface check.
var _ ReceiptVerifier = (*AppleReceiptVerifier)(nil)

func (v *AppleReceiptVerifier) VerifyPurchase(ctx context.Context, purchaseToken string) (*VerifyResult, error) {
	token, err := v.generateJWT()
	if err != nil {
		return nil, fmt.Errorf("generate JWT: %w", err)
	}

	url := fmt.Sprintf("%s/inApps/v2/transactions/%s", v.baseURL, purchaseToken)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to Apple: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return &VerifyResult{IsValid: false}, fmt.Errorf("Apple API returned %d (body read failed: %v)", resp.StatusCode, err)
		}
		return &VerifyResult{IsValid: false}, fmt.Errorf("Apple API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode Apple response: %w", err)
	}

	txnInfo, err := decodeJWSPayload(apiResp.SignedTransactionInfo)
	if err != nil {
		return nil, fmt.Errorf("decode transaction info: %w", err)
	}

	return &VerifyResult{
		IsValid:       true,
		TransactionID: txnInfo.TransactionID,
		ProductID:     txnInfo.ProductID,
		PurchaseTime:  time.UnixMilli(txnInfo.PurchaseDate),
	}, nil
}

func (v *AppleReceiptVerifier) VerifySubscription(ctx context.Context, purchaseToken string) (*SubscriptionInfo, error) {
	token, err := v.generateJWT()
	if err != nil {
		return nil, fmt.Errorf("generate JWT: %w", err)
	}

	url := fmt.Sprintf("%s/inApps/v2/subscriptions/%s", v.baseURL, purchaseToken)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to Apple: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return &SubscriptionInfo{IsValid: false}, fmt.Errorf("Apple API returned %d (body read failed: %v)", resp.StatusCode, err)
		}
		return &SubscriptionInfo{IsValid: false}, fmt.Errorf("Apple API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Data []struct {
			LastTransactions []struct {
				SignedTransactionInfo string `json:"signedTransactionInfo"`
				SignedRenewalInfo     string `json:"signedRenewalInfo"`
			} `json:"lastTransactions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode Apple subscription response: %w", err)
	}

	if len(apiResp.Data) == 0 || len(apiResp.Data[0].LastTransactions) == 0 {
		return &SubscriptionInfo{IsValid: false}, nil
	}

	lastTxn := apiResp.Data[0].LastTransactions[0]

	txnInfo, err := decodeJWSPayload(lastTxn.SignedTransactionInfo)
	if err != nil {
		return nil, fmt.Errorf("decode transaction info: %w", err)
	}

	renewalInfo, err := decodeJWSRenewalPayload(lastTxn.SignedRenewalInfo)
	if err != nil {
		return nil, fmt.Errorf("decode renewal info: %w", err)
	}

	return &SubscriptionInfo{
		IsValid:        true,
		ProductID:      txnInfo.ProductID,
		TransactionID:  txnInfo.TransactionID,
		ExpiresAt:      time.UnixMilli(txnInfo.ExpiresDate),
		IsAutoRenewing: renewalInfo.AutoRenewStatus == 1,
	}, nil
}

func (v *AppleReceiptVerifier) generateJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    v.issuerID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(20 * time.Minute)),
		Audience:  jwt.ClaimStrings{"appstoreconnect-v1"},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = v.keyID

	return token.SignedString(v.privateKey)
}

// appleTransactionInfo represents the decoded JWS payload from Apple.
type appleTransactionInfo struct {
	TransactionID string `json:"transactionId"`
	ProductID     string `json:"productId"`
	PurchaseDate  int64  `json:"purchaseDate"`
	ExpiresDate   int64  `json:"expiresDate"`
	BundleID      string `json:"bundleId"`
}

type appleRenewalInfo struct {
	AutoRenewStatus int `json:"autoRenewStatus"`
}

// decodeJWSPayload extracts the payload from a JWS without signature verification.
// In production, you should verify the signature against Apple's root certificate.
func decodeJWSPayload(jws string) (*appleTransactionInfo, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}

	var info appleTransactionInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		return nil, fmt.Errorf("unmarshal transaction info: %w", err)
	}
	return &info, nil
}

func decodeJWSRenewalPayload(jws string) (*appleRenewalInfo, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}

	var info appleRenewalInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		return nil, fmt.Errorf("unmarshal renewal info: %w", err)
	}
	return &info, nil
}
