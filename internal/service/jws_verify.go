package service

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
)

// appleRootCAG3PEM is Apple Root CA - G3 (ECDSA P-384).
// Downloaded from https://www.apple.com/certificateauthority/
const appleRootCAG3PEM = "" +
	"-----BEGIN CERTIFICATE-----\n" +
	"MIICQzCCAcmgAwIBAgIILcX8iNLFS5UwCgYIKoZIzj0EAwMwZzEbMBkGA1UEAwwS\n" +
	"QXBwbGUgUm9vdCBDQSAtIEczMSYwJAYDVQQLDB1BcHBsZSBDZXJ0aWZpY2F0aW9u\n" +
	"IEF1dGhvcml0eTETMBEGA1UECgwKQXBwbGUgSW5jLjELMAkGA1UEBhMCVVMwHhcN\n" +
	"MTQwNDMwMTgxOTA2WhcNMzkwNDMwMTgxOTA2WjBnMRswGQYDVQQDDBJBcHBsZSBS\n" +
	"b290IENBIC0gRzMxJjAkBgNVBAsMHUFwcGxlIENlcnRpZmljYXRpb24gQXV0aG9y\n" +
	"aXR5MRMwEQYDVQQKDApBcHBsZSBJbmMuMQswCQYDVQQGEwJVUzB2MBAGByqGSM49\n" +
	"AgEGBSuBBAAiA2IABJjpLz1AcqTtkyJygRMc3RCV8cWjTnHcFBbZDuWmBSp3ZHtf\n" +
	"TjjTuxxEtX/1H7YyYl3J6YRbTzBPEVoA/VhYDKX1DyxNB0cTddqXl5dvMVztK517\n" +
	"IDvYuVTZXpmkOlEKMaNCMEAwHQYDVR0OBBYEFLuw3qFYM4iapIqZ3r6966/ayySr\n" +
	"MA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMAoGCCqGSM49BAMDA2gA\n" +
	"MGUCMQCD6cHEFl4aXTQY2e3v9GwOAEZLuN+yRhHFD/3meoyhpmvOwgPUnPWTxnS4\n" +
	"at+qIxUCMG1mihDK1A3UT82NQz60imOlM27jbdoXt2QfyFMm+YhidDkLF1vLUagM\n" +
	"6BgD56KyKA==\n" +
	"-----END CERTIFICATE-----\n"

// jwsHeader represents the JOSE header of a JWS.
type jwsHeader struct {
	Alg string   `json:"alg"`
	X5C []string `json:"x5c"`
}

var appleRootPool *x509.CertPool

func init() {
	block, _ := pem.Decode([]byte(appleRootCAG3PEM))
	if block == nil {
		panic("Apple Root CA - G3: failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic("Apple Root CA - G3: " + err.Error())
	}
	appleRootPool = x509.NewCertPool()
	appleRootPool.AddCert(cert)
}

// verifyAppleJWS verifies an Apple JWS token's x5c certificate chain and
// ECDSA signature, then returns the raw payload bytes.
func verifyAppleJWS(jws string) ([]byte, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format: expected 3 parts, got %d", len(parts))
	}

	// Decode and parse the header.
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode JWS header: %w", err)
	}

	var header jwsHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("unmarshal JWS header: %w", err)
	}

	if header.Alg != "ES256" {
		return nil, fmt.Errorf("unsupported JWS algorithm: %s", header.Alg)
	}

	if len(header.X5C) == 0 {
		return nil, fmt.Errorf("JWS header missing x5c certificate chain")
	}

	// Build certificate chain from x5c (leaf first, root last).
	certs := make([]*x509.Certificate, len(header.X5C))
	for i, certB64 := range header.X5C {
		certDER, err := base64.StdEncoding.DecodeString(certB64)
		if err != nil {
			return nil, fmt.Errorf("decode x5c[%d]: %w", i, err)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return nil, fmt.Errorf("parse x5c[%d]: %w", i, err)
		}
		certs[i] = cert
	}

	// Verify the certificate chain against Apple's root CA.
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         appleRootPool,
		Intermediates: intermediates,
	}); err != nil {
		return nil, fmt.Errorf("verify x5c certificate chain: %w", err)
	}

	// Extract ECDSA public key from the leaf certificate.
	pubKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("leaf certificate public key is not ECDSA")
	}

	// Verify the ES256 signature.
	signingInput := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signingInput))

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode JWS signature: %w", err)
	}

	// ES256 signature is r || s, each 32 bytes.
	if len(sigBytes) != 64 {
		return nil, fmt.Errorf("invalid ES256 signature length: expected 64 bytes, got %d", len(sigBytes))
	}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	if !ecdsa.Verify(pubKey, hash[:], r, s) {
		return nil, fmt.Errorf("JWS signature verification failed")
	}

	// Decode and return the payload.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}

	return payload, nil
}

// jwsVerifyFunc is the function used to verify and extract JWS payloads.
// Overridden in tests to bypass Apple certificate verification.
var jwsVerifyFunc = verifyAppleJWS

// decodeVerifiedJWSPayload verifies an Apple JWS token's x5c certificate
// chain and ECDSA signature, then unmarshals the payload into T.
func decodeVerifiedJWSPayload[T any](jws string) (*T, error) {
	payload, err := jwsVerifyFunc(jws)
	if err != nil {
		return nil, err
	}

	var v T
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, fmt.Errorf("unmarshal JWS payload: %w", err)
	}
	return &v, nil
}
