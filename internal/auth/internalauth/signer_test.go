package internalauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateRSAPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func encodePKCS8PEM(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func parseIssuedToken(t *testing.T, tokenString string, pub *rsa.PublicKey) *jwt.Token {
	t.Helper()
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(tok *jwt.Token) (interface{}, error) {
		return pub, nil
	}, jwt.WithoutClaimsValidation())
	require.NoError(t, err)
	require.True(t, token.Valid)
	return token
}

func TestParsePrivateKeyPEM(t *testing.T) {
	t.Run("[内部認証]秘密鍵の読み取り", func(t *testing.T) {
		key := generateRSAPrivateKey(t)
		pkcs8PEM := encodePKCS8PEM(t, key)

		successTests := []struct {
			name string
			pem  []byte
		}{
			{
				name: "PKCS8形式でエンコードされたRSA秘密鍵をPEM形式で渡したとき、その秘密鍵が読み取れる",
				pem:  pkcs8PEM,
			},
			{
				name: "PEMブロックのあとに余分なデータが続くPEM形式を渡したとき、先頭のブロックから秘密鍵が読み取れる",
				pem:  append(append([]byte{}, pkcs8PEM...), []byte("trailing-garbage-data")...),
			},
		}
		for _, tt := range successTests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := ParsePrivateKeyPEM(tt.pem)

				require.NoError(t, err)
				assert.True(t, key.Equal(got))
			})
		}

		ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		notPKCS8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-a-valid-pkcs8-der")})

		errorTests := []struct {
			name string
			pem  []byte
		}{
			{
				name: "PEM形式でないバイト列を渡したとき、読み取りはエラーになる",
				pem:  []byte("not a pem encoded block"),
			},
			{
				name: "空のバイト列を渡したとき、読み取りはエラーになる",
				pem:  []byte{},
			},
			{
				name: "PEM形式ではあるが、中身がPKCS8として解釈できないとき、読み取りはエラーになる",
				pem:  notPKCS8PEM,
			},
			{
				name: "PKCS8として解釈できるが、RSA以外の秘密鍵(例:ECDSA鍵)であるとき、読み取りはエラーになる",
				pem:  encodePKCS8PEM(t, ecdsaKey),
			},
		}
		for _, tt := range errorTests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ParsePrivateKeyPEM(tt.pem)

				assert.Error(t, err)
			})
		}
	})
}

func TestStaticPrivateKeyResolver(t *testing.T) {
	t.Run("[内部認証]鍵識別子に基づく鍵の解決", func(t *testing.T) {
		t.Run("登録した鍵識別子と一致する鍵識別子を問い合わせたとき、登録した秘密鍵が返る", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			resolver := StaticPrivateKeyResolver(key, "v1")

			got, err := resolver("v1")

			require.NoError(t, err)
			assert.True(t, key.Equal(got))
		})

		t.Run("登録した鍵識別子と異なる鍵識別子を問い合わせたとき、解決はエラーになる", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			resolver := StaticPrivateKeyResolver(key, "v1")

			_, err := resolver("v2")

			assert.Error(t, err)
		})
	})
}

func TestIssue(t *testing.T) {
	t.Run("[内部認証]内部認証トークンの発行", func(t *testing.T) {
		t.Run("空文字でないプレイヤー識別子を指定したとき、発行される内部認証トークンの主体(sub)クレームがそのプレイヤー識別子になる", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			signer := NewSigner(StaticPrivateKeyResolver(key, DefaultKeyID), DefaultKeyID)

			tokenString, err := signer.Issue("player-123")

			require.NoError(t, err)
			token := parseIssuedToken(t, tokenString, &key.PublicKey)
			claims := token.Claims.(*jwt.RegisteredClaims)
			assert.Equal(t, "player-123", claims.Subject)
		})

		t.Run("プレイヤー識別子が空文字のとき、発行はエラーになる", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			signer := NewSigner(StaticPrivateKeyResolver(key, DefaultKeyID), DefaultKeyID)

			_, err := signer.Issue("")

			assert.Error(t, err)
		})

		t.Run("発行に使う鍵の解決が失敗するとき(指定した鍵識別子に対応する鍵が登録されていないとき)、発行はエラーになる", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			signer := NewSigner(StaticPrivateKeyResolver(key, "registered-key-id"), "different-key-id")

			_, err := signer.Issue("player-123")

			assert.Error(t, err)
		})

		t.Run("発行された内部認証トークンの発行者(iss)クレームは\"overload-party-gateway\"になる", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			signer := NewSigner(StaticPrivateKeyResolver(key, DefaultKeyID), DefaultKeyID)

			tokenString, err := signer.Issue("player-123")

			require.NoError(t, err)
			token := parseIssuedToken(t, tokenString, &key.PublicKey)
			claims := token.Claims.(*jwt.RegisteredClaims)
			assert.Equal(t, "overload-party-gateway", claims.Issuer)
		})

		t.Run("発行された内部認証トークンには、発行時刻を基準として5分後に失効する有効期限が設定される", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			fixedNow := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
			signer := NewSigner(StaticPrivateKeyResolver(key, DefaultKeyID), DefaultKeyID, WithClock(func() time.Time { return fixedNow }))

			tokenString, err := signer.Issue("player-123")
			require.NoError(t, err)

			token := parseIssuedToken(t, tokenString, &key.PublicKey)
			claims := token.Claims.(*jwt.RegisteredClaims)
			assert.Equal(t, fixedNow.Add(5*time.Minute), claims.ExpiresAt.UTC())
		})

		t.Run("発行された内部認証トークンのヘッダーには、署名に使った鍵を識別する情報(kid)が、その鍵の識別子と一致する値で含まれる", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			const keyID KeyID = "test-key-id"
			signer := NewSigner(StaticPrivateKeyResolver(key, keyID), keyID)

			tokenString, err := signer.Issue("player-123")

			require.NoError(t, err)
			token := parseIssuedToken(t, tokenString, &key.PublicKey)
			assert.Equal(t, string(keyID), token.Header["kid"])
		})

		t.Run("発行された内部認証トークンはRS256で署名されている", func(t *testing.T) {
			key := generateRSAPrivateKey(t)
			signer := NewSigner(StaticPrivateKeyResolver(key, DefaultKeyID), DefaultKeyID)

			tokenString, err := signer.Issue("player-123")

			require.NoError(t, err)
			token := parseIssuedToken(t, tokenString, &key.PublicKey)
			assert.Equal(t, "RS256", token.Method.Alg())
		})
	})
}
