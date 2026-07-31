// Package internalauth は内部サービス間認証 JWT (RS256) の検証を提供する共有パッケージ。
package internalauth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v4"
)

// PublicKeyResolver は kid に対応する検証用公開鍵を返す。
type PublicKeyResolver func(kid KeyID) (*rsa.PublicKey, error)

// ParsePublicKeyPEM は PEM 形式の RSA 公開鍵を読み取る。
func ParsePublicKeyPEM(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("internalauth: public key is not PEM encoded")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("internalauth: parse public key: %w", err)
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("internalauth: public key is %T, want RSA", parsed)
	}
	return key, nil
}

// StaticPublicKeyResolver は単一鍵だけを返す PublicKeyResolver を構築する。
func StaticPublicKeyResolver(key *rsa.PublicKey, keyID KeyID) PublicKeyResolver {
	return func(kid KeyID) (*rsa.PublicKey, error) {
		if kid != keyID {
			return nil, fmt.Errorf("internalauth: unknown key id %q", kid)
		}
		return key, nil
	}
}

// VerifierOption は NewVerifier の任意フィールドを上書きする。
type VerifierOption func(*rs256Verifier)

// WithExpectedIssuer は受理する iss を上書きする。
func WithExpectedIssuer(issuer string) VerifierOption {
	return func(v *rs256Verifier) { v.issuer = issuer }
}

// NewVerifier は RS256 の Verifier を構築する。
func NewVerifier(resolver PublicKeyResolver, opts ...VerifierOption) Verifier {
	v := &rs256Verifier{
		resolver: resolver,
		issuer:   ExpectedIssuer,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// rs256Verifier は RS256 で内部認証 JWT を検証し sub クレームを返す。
type rs256Verifier struct {
	resolver PublicKeyResolver
	issuer   string
}

var _ Verifier = (*rs256Verifier)(nil)

// Verify は signed token を検証し sub (= playerID) を返す。
// 署名 / 有効期限 / iss / alg=RS256 / kid 解決のいずれかが失敗すれば error を返す。
func (v *rs256Verifier) Verify(token string) (string, error) {
	if token == "" {
		return "", errors.New("internalauth: token is empty")
	}
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("internalauth: unexpected signing method %q", t.Header["alg"])
		}
		rawKID, ok := t.Header["kid"].(string)
		if !ok || rawKID == "" {
			return nil, errors.New("internalauth: kid header missing")
		}
		key, err := v.resolver(KeyID(rawKID))
		if err != nil {
			return nil, err
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
	if err != nil {
		return "", fmt.Errorf("internalauth: parse: %w", err)
	}
	if !parsed.Valid {
		return "", errors.New("internalauth: token invalid")
	}
	if claims.Issuer != v.issuer {
		return "", fmt.Errorf("internalauth: unexpected issuer %q", claims.Issuer)
	}
	if claims.Subject == "" {
		return "", errors.New("internalauth: sub is empty")
	}
	return claims.Subject, nil
}
