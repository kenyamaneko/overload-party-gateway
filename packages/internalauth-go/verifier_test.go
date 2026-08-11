package internalauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func encodeRSAPublicKeyPKIXPEM(t *testing.T, pub *rsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func strPtr(s string) *string {
	return &s
}

const (
	testKeyID     = KeyID("test-key-id")
	defaultIssuer = "overload-party-gateway"
)

func validTestClaims(sub string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"sub": sub,
		"iss": defaultIssuer,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
}

func signTestToken(t *testing.T, method jwt.SigningMethod, key any, kid *string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	if kid != nil {
		token.Header["kid"] = *kid
	}
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func tamperTokenSignature(token string) string {
	parts := strings.Split(token, ".")
	sig := []rune(parts[2])
	if sig[0] == 'A' {
		sig[0] = 'Z'
	} else {
		sig[0] = 'A'
	}
	parts[2] = string(sig)
	return strings.Join(parts, ".")
}

func TestParsePublicKeyPEM(t *testing.T) {
	t.Run("PEM形式の公開鍵の読み取り", func(t *testing.T) {
		t.Run("正しいPEM形式でエンコードされたRSA公開鍵を渡すと、対応するRSA公開鍵が得られる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			pemBytes := encodeRSAPublicKeyPKIXPEM(t, &key.PublicKey)

			got, err := ParsePublicKeyPEM(pemBytes)

			require.NoError(t, err)
			assert.True(t, got.Equal(&key.PublicKey))
		})

		t.Run("PEM形式でないデータ(空データを含む)を渡すと、エラーになる", func(t *testing.T) {
			tests := []struct {
				name  string
				input []byte
			}{
				{name: "空データのとき", input: []byte{}},
				{name: "PEM形式でない文字列(ゴミデータ)のとき", input: []byte("not a pem encoded value")},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					_, err := ParsePublicKeyPEM(tt.input)

					assert.Error(t, err)
				})
			}
		})

		t.Run("PEM形式ではあるが、中身がRSA公開鍵ではない(別の鍵種別、または公開鍵以外のデータ)とき、エラーになる", func(t *testing.T) {
			t.Run("別の鍵種別(ECDSA公開鍵)のとき", func(t *testing.T) {
				ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				der, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
				require.NoError(t, err)
				pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

				_, err = ParsePublicKeyPEM(pemBytes)

				assert.Error(t, err)
			})

			t.Run("公開鍵以外のデータ(RSA秘密鍵)のとき", func(t *testing.T) {
				key := generateTestRSAKey(t)
				der := x509.MarshalPKCS1PrivateKey(key)
				pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})

				_, err := ParsePublicKeyPEM(pemBytes)

				assert.Error(t, err)
			})
		})
	})
}

func TestStaticPublicKeyResolver(t *testing.T) {
	t.Run("鍵識別子に基づく公開鍵の解決", func(t *testing.T) {
		t.Run("登録しておいた鍵と鍵識別子の組に対して、その鍵識別子を問い合わせると、登録しておいた公開鍵が得られる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			resolver := StaticPublicKeyResolver(&key.PublicKey, testKeyID)

			got, err := resolver(testKeyID)

			require.NoError(t, err)
			assert.True(t, got.Equal(&key.PublicKey))
		})

		t.Run("登録した鍵識別子と異なる鍵識別子を問い合わせると、エラーになる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			resolver := StaticPublicKeyResolver(&key.PublicKey, testKeyID)

			_, err := resolver(KeyID("other-key-id"))

			assert.Error(t, err)
		})
	})
}

func TestRS256VerifierVerify(t *testing.T) {
	t.Run("内部認証トークンの署名・有効期限・発行者の検証", func(t *testing.T) {
		t.Run("空文字列の内部認証トークンを渡すと、エラーになり、エラーの内容から内部認証トークンが空であることが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))

			_, err := verifier.Verify("")

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "token is empty")
		})

		t.Run("署名アルゴリズムがRS256以外の内部認証トークンを渡すと、エラーになり、エラーの内容から署名アルゴリズムが原因であることが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			kid := string(testKeyID)
			token := signTestToken(t, jwt.SigningMethodHS256, []byte("hmac-secret"), &kid, validTestClaims("player-001"))

			_, err := verifier.Verify(token)

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "signing method")
		})

		t.Run("内部認証トークンのヘッダに鍵識別子(kid)が無い、または空のとき、エラーになり、エラーの内容から鍵識別子が無いことが原因であることが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))

			tests := []struct {
				name string
				kid  *string
			}{
				{name: "kidヘッダが無いとき", kid: nil},
				{name: "kidヘッダが空文字列のとき", kid: strPtr("")},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					token := signTestToken(t, jwt.SigningMethodRS256, key, tt.kid, validTestClaims("player-001"))

					_, err := verifier.Verify(token)

					require.Error(t, err)
					assert.Contains(t, strings.ToLower(err.Error()), "kid header missing")
				})
			}
		})

		t.Run("内部認証トークンのヘッダの鍵識別子が、検証側で解決できない(未知の)鍵識別子のとき、エラーになり、エラーの内容からその鍵識別子が解決できなかったことが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			unknownKid := "unknown-key-id"
			token := signTestToken(t, jwt.SigningMethodRS256, key, &unknownKid, validTestClaims("player-001"))

			_, err := verifier.Verify(token)

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "unknown key id")
			assert.Contains(t, err.Error(), unknownKid)
		})

		t.Run("有効な形式だが、対応する公開鍵で検証できない(改ざんされている、または別の秘密鍵で署名されている)内部認証トークンを渡すと、エラーになり、エラーの内容から署名が無効であることが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			otherKey := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			kid := string(testKeyID)

			t.Run("改ざんされているとき", func(t *testing.T) {
				token := signTestToken(t, jwt.SigningMethodRS256, key, &kid, validTestClaims("player-001"))
				tampered := tamperTokenSignature(token)

				_, err := verifier.Verify(tampered)

				require.Error(t, err)
				assert.Contains(t, strings.ToLower(err.Error()), "verification error")
			})

			t.Run("別の秘密鍵で署名されているとき", func(t *testing.T) {
				token := signTestToken(t, jwt.SigningMethodRS256, otherKey, &kid, validTestClaims("player-001"))

				_, err := verifier.Verify(token)

				require.Error(t, err)
				assert.Contains(t, strings.ToLower(err.Error()), "verification error")
			})
		})

		t.Run("有効期限が過去の時刻になっている内部認証トークンを渡すと、エラーになり、エラーの内容から有効期限切れが原因であることが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			kid := string(testKeyID)
			claims := validTestClaims("player-001")
			claims["exp"] = time.Now().Add(-time.Hour).Unix()
			token := signTestToken(t, jwt.SigningMethodRS256, key, &kid, claims)

			_, err := verifier.Verify(token)

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "is expired")
		})

		t.Run("issクレームが期待する発行者と一致しない内部認証トークンを渡すと、エラーになり、エラーの内容から発行者が一致しないことが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			kid := string(testKeyID)
			claims := validTestClaims("player-001")
			claims["iss"] = "unexpected-issuer"
			token := signTestToken(t, jwt.SigningMethodRS256, key, &kid, claims)

			_, err := verifier.Verify(token)

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "unexpected issuer")
		})

		t.Run("検証器を構築する際に想定する発行者を明示的に指定しなかった場合、既定の発行者\"overload-party-gateway\"が期待値として使われ、issクレームがこれと一致する内部認証トークンは検証に成功する", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			kid := string(testKeyID)
			claims := validTestClaims("player-001")
			claims["iss"] = defaultIssuer
			token := signTestToken(t, jwt.SigningMethodRS256, key, &kid, claims)

			playerID, err := verifier.Verify(token)

			require.NoError(t, err)
			assert.Equal(t, "player-001", playerID)
		})

		t.Run("検証器を構築する際に想定する発行者を明示的に指定した場合、その指定した値が期待値として使われる(既定の発行者ではなく指定した値と一致する内部認証トークンだけが成功する)", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID), WithExpectedIssuer("custom-issuer"))
			kid := string(testKeyID)
			claims := validTestClaims("player-001")
			claims["iss"] = "custom-issuer"
			token := signTestToken(t, jwt.SigningMethodRS256, key, &kid, claims)

			playerID, err := verifier.Verify(token)

			require.NoError(t, err)
			assert.Equal(t, "player-001", playerID)
		})

		t.Run("subクレームが空文字列の内部認証トークンを渡すと、エラーになり、エラーの内容からsubクレームが空であることが分かる", func(t *testing.T) {
			key := generateTestRSAKey(t)
			verifier := NewVerifier(StaticPublicKeyResolver(&key.PublicKey, testKeyID))
			kid := string(testKeyID)
			claims := validTestClaims("")
			token := signTestToken(t, jwt.SigningMethodRS256, key, &kid, claims)

			_, err := verifier.Verify(token)

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "sub is empty")
		})
	})
}
