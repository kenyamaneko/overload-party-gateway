// Package internalauth は内部サービス間認証 JWT (RS256) の発行を提供する。
package internalauth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Issuer は本パッケージが発行する JWT の iss クレーム値。
const Issuer = "overload-party-gateway"

// HeaderName は内部認証 JWT を運ぶ HTTP ヘッダ名。
const HeaderName = "X-Internal-Auth"

// DefaultTTL は発行する JWT の生存期間。
const DefaultTTL = 5 * time.Minute

// DefaultKeyID は署名鍵を識別する初期 key ID。
const DefaultKeyID KeyID = "v1"

// KeyID は署名鍵を識別する文字列。
type KeyID string

// PrivateKeyResolver は kid に対応する署名用秘密鍵を返す。
type PrivateKeyResolver func(kid KeyID) (*rsa.PrivateKey, error)

// ParsePrivateKeyPEM は PEM 形式の RSA 秘密鍵を読み取る。
func ParsePrivateKeyPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("internalauth: private key is not PEM encoded")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("internalauth: parse private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("internalauth: private key is %T, want RSA", parsed)
	}
	return key, nil
}

// StaticPrivateKeyResolver は単一鍵だけを返す PrivateKeyResolver を構築する。
func StaticPrivateKeyResolver(key *rsa.PrivateKey, keyID KeyID) PrivateKeyResolver {
	return func(kid KeyID) (*rsa.PrivateKey, error) {
		if kid != keyID {
			return nil, fmt.Errorf("internalauth: unknown key id %q", kid)
		}
		return key, nil
	}
}

// Signer は RS256 で内部認証 JWT を発行する。
type Signer struct {
	resolver PrivateKeyResolver
	keyID    KeyID
	issuer   string
	ttl      time.Duration
	now      func() time.Time
}

// Option は Signer の任意フィールドを上書きする。
type Option func(*Signer)

// WithIssuer は iss クレーム値を上書きする。
func WithIssuer(issuer string) Option {
	return func(s *Signer) { s.issuer = issuer }
}

// WithTTL は exp - iat の差を上書きする。
func WithTTL(ttl time.Duration) Option {
	return func(s *Signer) { s.ttl = ttl }
}

// WithClock は時刻取得関数を上書きする。
func WithClock(now func() time.Time) Option {
	return func(s *Signer) { s.now = now }
}

// NewSigner は PrivateKeyResolver と KeyID から Signer を生成する。
func NewSigner(resolver PrivateKeyResolver, keyID KeyID, opts ...Option) *Signer {
	s := &Signer{
		resolver: resolver,
		keyID:    keyID,
		issuer:   Issuer,
		ttl:      DefaultTTL,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Issue は playerID を sub クレームに含む RS256 署名済 JWT を発行する。
func (s *Signer) Issue(playerID string) (string, error) {
	if playerID == "" {
		return "", errors.New("internalauth: playerID is empty")
	}
	key, err := s.resolver(s.keyID)
	if err != nil {
		return "", fmt.Errorf("internalauth: resolve key: %w", err)
	}
	now := s.now()
	claims := jwt.RegisteredClaims{
		Subject:   playerID,
		Issuer:    s.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = string(s.keyID)
	signed, err := tok.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("internalauth: sign: %w", err)
	}
	return signed, nil
}
