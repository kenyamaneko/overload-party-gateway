package internalauth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSecretValue = "test-internal-auth-secret-do-not-use-in-prod-xxxxx"
	testPlayerID    = "player-123"
)

// signWithKid は HS256 で kid header 付き JWT を組み立てる test helper。
func signWithKid(t *testing.T, secret []byte, kid string, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(secret)
	require.NoError(t, err)
	return signed
}

// signWithoutKid は kid header を持たない HS256 JWT を組み立てる test helper。
func signWithoutKid(t *testing.T, secret []byte, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
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
	secret := []byte(testSecretValue)
	v := NewVerifier(StaticHS256Resolver(secret, DefaultKeyID))

	t.Run("JWT の検証", func(t *testing.T) {
		t.Run("有効な JWT のとき、sub を player_id として返す", func(t *testing.T) {
			token := signWithKid(t, secret, string(DefaultKeyID), validClaims(time.Now()))

			got, err := v.Verify(token)
			require.NoError(t, err)
			assert.Equal(t, testPlayerID, got)
		})

		t.Run("期限切れ JWT のとき、ErrTokenExpired になる", func(t *testing.T) {
			now := time.Now()
			expiredToken := signWithKid(t, secret, string(DefaultKeyID), jwt.RegisteredClaims{
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

		rejectCases := []struct {
			name  string
			token string
		}{
			{
				name:  "署名が不一致のとき、拒否される",
				token: signWithKid(t, []byte("wrong-secret-for-signing-must-be-long-enough-32b"), string(DefaultKeyID), validClaims(now)),
			},
			{
				name:  "空の token のとき、拒否される",
				token: "",
			},
			{
				name:  "JWT として parse できない token のとき、拒否される",
				token: "not-a-jwt",
			},
			{
				name:  "kid header が無いとき、拒否される",
				token: signWithoutKid(t, secret, validClaims(now)),
			},
			{
				name:  "未知の kid のとき、拒否される",
				token: signWithKid(t, secret, "v999", validClaims(now)),
			},
			{
				name: "想定外の iss のとき、拒否される",
				token: signWithKid(t, secret, string(DefaultKeyID), jwt.RegisteredClaims{
					Subject:   testPlayerID,
					Issuer:    "evil-issuer",
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
				}),
			},
			{
				name:  "alg=none のとき、拒否される",
				token: noneAlgToken,
			},
			{
				name: "sub が空のとき、拒否される",
				token: signWithKid(t, secret, string(DefaultKeyID), jwt.RegisteredClaims{
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

func TestStaticHS256Resolver(t *testing.T) {
	secret := []byte(testSecretValue)

	t.Run("HS256 鍵の解決", func(t *testing.T) {
		t.Run("登録済みの kid のとき、secret を返す", func(t *testing.T) {
			resolver := StaticHS256Resolver(secret, DefaultKeyID)

			got, err := resolver(DefaultKeyID)
			require.NoError(t, err)
			assert.Equal(t, secret, got)
		})

		t.Run("未知の kid のとき、エラーになる", func(t *testing.T) {
			resolver := StaticHS256Resolver(secret, DefaultKeyID)
			_, err := resolver(KeyID("v999"))
			require.Error(t, err)
		})
	})
}
