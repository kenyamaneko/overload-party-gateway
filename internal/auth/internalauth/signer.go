// Package internalauth は内部サービス間認証 JWT (HS256) の発行を提供する。
package internalauth

import (
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

// DefaultKeyID は HMAC 鍵を識別する初期 key ID。
const DefaultKeyID KeyID = "v1"

// KeyID は HMAC 鍵を識別する文字列。
type KeyID string

// KeyResolver は kid に対応する HMAC 鍵を返す。
type KeyResolver func(kid KeyID) ([]byte, error)

// StaticHS256Resolver は単一鍵だけを返す KeyResolver を構築する。
func StaticHS256Resolver(secret []byte, keyID KeyID) KeyResolver {
	return func(kid KeyID) ([]byte, error) {
		if kid != keyID {
			return nil, fmt.Errorf("internalauth: unknown key id %q", kid)
		}
		return secret, nil
	}
}

// Signer は HS256 で内部認証 JWT を発行する。
type Signer struct {
	resolver KeyResolver
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

// NewSigner は KeyResolver と KeyID から Signer を生成する。
func NewSigner(resolver KeyResolver, keyID KeyID, opts ...Option) *Signer {
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

// Issue は playerID を sub クレームに含む HS256 署名済 JWT を発行する。
func (s *Signer) Issue(playerID string) (string, error) {
	if playerID == "" {
		return "", errors.New("internalauth: playerID is empty")
	}
	secret, err := s.resolver(s.keyID)
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
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = string(s.keyID)
	signed, err := tok.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("internalauth: sign: %w", err)
	}
	return signed, nil
}
