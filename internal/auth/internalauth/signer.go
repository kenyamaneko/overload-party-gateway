// Package internalauth は ADR-037 で定義した内部サービス間認証 (HMAC 署名 JWT) の発行を担う。
//
// gateway は Firebase ID Token 検証後に本パッケージで JWT を発行し、X-Internal-Auth ヘッダで
// 下流サービスへ引き渡す。検証側 (各サービス) は本パッケージを利用しないため、公開 API は
// 発行関連のみで構成する。
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

// DefaultTTL は発行する JWT の生存期間。ADR-037 で 5 分と定めた。
const DefaultTTL = 5 * time.Minute

// DefaultKeyID は鍵ローテーション余地のために予約された初期 key ID。
const DefaultKeyID KeyID = "v1"

// KeyID は HMAC 鍵を識別する文字列。JWT header の kid に書き込む。
type KeyID string

// KeyResolver は kid に対応する HMAC 鍵を返す。
//
// 将来 RS256 へ移行する際は同シグネチャで秘密鍵 (発行側) / 公開鍵 (検証側) を返す形に
// 拡張可能であり、Signer を含む発行/検証経路の差し替えを最小化する。
type KeyResolver func(kid KeyID) ([]byte, error)

// StaticHS256Resolver は単一鍵だけを返す KeyResolver を構築する。
//
// Phase 1 では env (INTERNAL_AUTH_SECRET) で配布する 1 鍵運用のため本リゾルバで足りる。
// 指定 keyID と異なる kid が渡された場合はエラーを返し、誤設定を fail-fast で検出する。
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

// WithClock は時刻取得関数を上書きする。テストでの固定時刻注入に用いる。
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
