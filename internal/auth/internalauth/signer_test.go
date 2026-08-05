package internalauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestKey はテスト専用の RSA 鍵を生成する。鍵長は検証速度を優先して 2048 bit にする。
func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func TestSignerIssue(t *testing.T) {
	key := newTestKey(t)
	fixedTime := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	t.Run("JWT の発行", func(t *testing.T) {
		validCases := []struct {
			name      string
			opts      []Option
			assertTok func(t *testing.T, parsed *jwt.Token, claims *jwt.RegisteredClaims)
		}{
			{
				name: "デフォルトオプションのとき、RS256 で iss/sub/iat/exp/kid が揃った JWT を発行する",
				opts: []Option{WithClock(func() time.Time { return fixedTime })},
				assertTok: func(t *testing.T, parsed *jwt.Token, claims *jwt.RegisteredClaims) {
					t.Helper()
					assert.Equal(t, "RS256", parsed.Method.Alg())
					assert.Equal(t, string(DefaultKeyID), parsed.Header["kid"])
					assert.Equal(t, "player-123", claims.Subject)
					assert.Equal(t, Issuer, claims.Issuer)
					assert.True(t, claims.IssuedAt.Equal(fixedTime))
					assert.Equal(t, DefaultTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
				},
			},
			{
				name: "TTL を指定したとき、exp-iat が指定した値になる",
				opts: []Option{WithClock(func() time.Time { return fixedTime }), WithTTL(2 * time.Minute)},
				assertTok: func(t *testing.T, _ *jwt.Token, claims *jwt.RegisteredClaims) {
					t.Helper()
					assert.Equal(t, 2*time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
				},
			},
			{
				name: "issuer を指定したとき、iss が上書きされる",
				opts: []Option{WithClock(func() time.Time { return fixedTime }), WithIssuer("other-issuer")},
				assertTok: func(t *testing.T, _ *jwt.Token, claims *jwt.RegisteredClaims) {
					t.Helper()
					assert.Equal(t, "other-issuer", claims.Issuer)
				},
			},
		}

		for _, tc := range validCases {
			t.Run(tc.name, func(t *testing.T) {
				signer := NewSigner(StaticPrivateKeyResolver(key, DefaultKeyID), DefaultKeyID, tc.opts...)
				token, err := signer.Issue("player-123")
				require.NoError(t, err)
				parsed, claims := parseAndAssertSignature(t, token, &key.PublicKey)
				tc.assertTok(t, parsed, claims)
			})
		}

		invalidCases := []struct {
			name     string
			playerID string
			resolver PrivateKeyResolver
		}{
			{
				name:     "playerID が空のとき、エラーになる",
				playerID: "",
				resolver: StaticPrivateKeyResolver(key, DefaultKeyID),
			},
			{
				name:     "resolver がエラーを返すとき、そのエラーが伝搬する",
				playerID: "player-123",
				resolver: func(KeyID) (*rsa.PrivateKey, error) { return nil, errors.New("boom") },
			},
		}

		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				signer := NewSigner(tc.resolver, DefaultKeyID)
				_, err := signer.Issue(tc.playerID)
				require.Error(t, err)
			})
		}
	})
}

func TestStaticPrivateKeyResolver(t *testing.T) {
	t.Run("秘密鍵の解決", func(t *testing.T) {
		t.Run("未知の kid を渡すとき、エラーになる", func(t *testing.T) {
			resolver := StaticPrivateKeyResolver(newTestKey(t), DefaultKeyID)
			_, err := resolver(KeyID("v2"))
			require.Error(t, err)
		})
	})
}

func TestParsePrivateKeyPEM(t *testing.T) {
	t.Run("PEM 秘密鍵の読み取り", func(t *testing.T) {
		t.Run("正しい PEM のとき、発行した JWT が対応する公開鍵で検証できる", func(t *testing.T) {
			key := newTestKey(t)
			der, err := x509.MarshalPKCS8PrivateKey(key)
			require.NoError(t, err)
			encoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

			parsed, err := ParsePrivateKeyPEM(encoded)
			require.NoError(t, err)

			signer := NewSigner(StaticPrivateKeyResolver(parsed, DefaultKeyID), DefaultKeyID)
			token, err := signer.Issue("player-123")
			require.NoError(t, err)

			_, claims := parseAndAssertSignature(t, token, &key.PublicKey)
			assert.Equal(t, "player-123", claims.Subject)
		})

		t.Run("PEM でないとき、エラーになる", func(t *testing.T) {
			_, err := ParsePrivateKeyPEM([]byte("not-a-pem"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "PEM")
		})

		t.Run("RSA 以外の鍵のとき、エラーになる", func(t *testing.T) {
			_, priv, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			der, err := x509.MarshalPKCS8PrivateKey(priv)
			require.NoError(t, err)
			encoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

			_, err = ParsePrivateKeyPEM(encoded)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "want RSA")
		})
	})
}

func parseAndAssertSignature(t *testing.T, token string, key *rsa.PublicKey) (*jwt.Token, *jwt.RegisteredClaims) {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	// 固定時刻で発行した token を実時刻で検証すると iat/exp が衝突するため時刻検証を無効化する。
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsed, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return key, nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	return parsed, claims
}
