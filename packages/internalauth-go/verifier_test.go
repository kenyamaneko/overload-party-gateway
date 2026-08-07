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

const testPlayerID = "player-123"

// newTestKey はテスト専用の RSA 鍵を生成する。鍵長は検証速度を優先して 2048 bit にする。
func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

// newTestEd25519PublicKey は RSA 以外の鍵を用意する test helper。
func newTestEd25519PublicKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pub
}

// signWithKid は RS256 で kid header 付き JWT を組み立てる test helper。
func signWithKid(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	require.NoError(t, err)
	return signed
}

// signWithoutKid は kid header を持たない RS256 JWT を組み立てる test helper。
func signWithoutKid(t *testing.T, key *rsa.PrivateKey, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(key)
	require.NoError(t, err)
	return signed
}

func validClaims(now time.Time) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Subject:   testPlayerID,
		Issuer:    ExpectedIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}
}

func TestVerifier_Verify(t *testing.T) {
	key := newTestKey(t)
	v := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, DefaultKeyID))

	t.Run("[内部認証]JWTの検証", func(t *testing.T) {
		t.Run("有効なJWTのとき、subをplayer_idとして返す", func(t *testing.T) {
			token := signWithKid(t, key, string(DefaultKeyID), validClaims(time.Now()))

			got, err := v.Verify(token)
			require.NoError(t, err)
			assert.Equal(t, testPlayerID, got)
		})

		t.Run("期限切れJWTのとき、ErrTokenExpiredになる", func(t *testing.T) {
			now := time.Now()
			expiredToken := signWithKid(t, key, string(DefaultKeyID), jwt.RegisteredClaims{
				Subject:   testPlayerID,
				Issuer:    ExpectedIssuer,
				IssuedAt:  jwt.NewNumericDate(now.Add(-1 * time.Hour)),
				ExpiresAt: jwt.NewNumericDate(now.Add(-30 * time.Minute)),
			})

			_, err := v.Verify(expiredToken)
			require.Error(t, err)
			assert.True(t, errors.Is(err, jwt.ErrTokenExpired), "expected ErrTokenExpired, got %v", err)
		})

		now := time.Now()

		// alg=none 攻撃用の token を事前生成する。
		noneAlgTok := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims(now))
		noneAlgTok.Header["kid"] = string(DefaultKeyID)
		noneAlgToken, err := noneAlgTok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)

		// 対称鍵で署名した token を事前生成する。公開鍵を HMAC の秘密鍵として
		// 扱わせる alg 混同攻撃を拒否することの確認に使う。
		hmacTok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims(now))
		hmacTok.Header["kid"] = string(DefaultKeyID)
		publicKeyDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		require.NoError(t, err)
		hmacToken, err := hmacTok.SignedString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER}))
		require.NoError(t, err)

		rejectCases := []struct {
			name  string
			token string
		}{
			{
				name:  "別の鍵で署名されているとき、拒否される",
				token: signWithKid(t, newTestKey(t), string(DefaultKeyID), validClaims(now)),
			},
			{
				name:  "空のtokenのとき、拒否される",
				token: "",
			},
			{
				name:  "JWTとしてparseできないtokenのとき、拒否される",
				token: "not-a-jwt",
			},
			{
				name:  "kidヘッダーが無いとき、拒否される",
				token: signWithoutKid(t, key, validClaims(now)),
			},
			{
				name:  "未知のkidのとき、拒否される",
				token: signWithKid(t, key, "v999", validClaims(now)),
			},
			{
				name: "想定外のissのとき、拒否される",
				token: signWithKid(t, key, string(DefaultKeyID), jwt.RegisteredClaims{
					Subject:   testPlayerID,
					Issuer:    "evil-issuer",
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
				}),
			},
			{
				name:  "alg=noneのとき、拒否される",
				token: noneAlgToken,
			},
			{
				name:  "公開鍵を鍵にしたHS256で署名されているとき、拒否される",
				token: hmacToken,
			},
			{
				name: "subが空のとき、拒否される",
				token: signWithKid(t, key, string(DefaultKeyID), jwt.RegisteredClaims{
					Subject:   "",
					Issuer:    ExpectedIssuer,
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
				}),
			},
		}

		for _, tc := range rejectCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := v.Verify(tc.token)
				require.Error(t, err)
			})
		}
	})
}

func TestStaticPublicKeyResolver(t *testing.T) {
	key := newTestKey(t)

	t.Run("[内部認証]公開鍵の解決", func(t *testing.T) {
		t.Run("登録済みのkidのとき、公開鍵を返す", func(t *testing.T) {
			resolver := StaticPublicKeyResolver(&key.PublicKey, DefaultKeyID)

			got, err := resolver(DefaultKeyID)
			require.NoError(t, err)
			assert.Equal(t, &key.PublicKey, got)
		})

		t.Run("未知のkidのとき、エラーになる", func(t *testing.T) {
			resolver := StaticPublicKeyResolver(&key.PublicKey, DefaultKeyID)
			_, err := resolver(KeyID("v999"))
			require.Error(t, err)
		})
	})
}

func TestParsePublicKeyPEM(t *testing.T) {
	t.Run("[内部認証]PEM公開鍵の読み取り", func(t *testing.T) {
		t.Run("正しいPEMのとき、署名の検証に使える鍵が得られる", func(t *testing.T) {
			key := newTestKey(t)
			der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
			require.NoError(t, err)
			encoded := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

			parsed, err := ParsePublicKeyPEM(encoded)
			require.NoError(t, err)

			token := signWithKid(t, key, string(DefaultKeyID), validClaims(time.Now()))
			got, err := NewVerifier(StaticPublicKeyResolver(parsed, DefaultKeyID)).Verify(token)
			require.NoError(t, err)
			assert.Equal(t, testPlayerID, got)
		})

		t.Run("PEMでないとき、エラーになる", func(t *testing.T) {
			_, err := ParsePublicKeyPEM([]byte("not-a-pem"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "PEM")
		})

		t.Run("RSA以外の鍵のとき、エラーになる", func(t *testing.T) {
			der, err := x509.MarshalPKIXPublicKey(newTestEd25519PublicKey(t))
			require.NoError(t, err)
			encoded := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

			_, err = ParsePublicKeyPEM(encoded)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "want RSA")
		})
	})
}
