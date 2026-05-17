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

func TestVerifier_Verify_Success(t *testing.T) {
	secret := []byte(testSecretValue)
	v := NewVerifier(StaticHS256Resolver(secret, DefaultKeyID))
	token := signWithKid(t, secret, string(DefaultKeyID), validClaims(time.Now()))

	got, err := v.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, testPlayerID, got)
}

func TestVerifier_Verify_ExpiredReturnsErrTokenExpired(t *testing.T) {
	secret := []byte(testSecretValue)
	now := time.Now()
	expiredToken := signWithKid(t, secret, string(DefaultKeyID), jwt.RegisteredClaims{
		Subject:   testPlayerID,
		Issuer:    ExpectedIssuer,
		IssuedAt:  jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(now.Add(-30 * time.Minute)),
	})

	v := NewVerifier(StaticHS256Resolver(secret, DefaultKeyID))
	_, err := v.Verify(expiredToken)
	require.Error(t, err)
	assert.True(t, errors.Is(err, jwt.ErrTokenExpired), "expected ErrTokenExpired, got %v", err)
}

func TestVerifier_Verify_Error(t *testing.T) {
	secret := []byte(testSecretValue)
	now := time.Now()

	// alg=none 攻撃用の token を事前生成する。
	noneAlgTok := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims(now))
	noneAlgTok.Header["kid"] = string(DefaultKeyID)
	noneAlgToken, err := noneAlgTok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	cases := []struct {
		name  string
		token string
	}{
		{
			name:  "署名が不一致なら拒否される",
			token: signWithKid(t, []byte("wrong-secret-for-signing-must-be-long-enough-32b"), string(DefaultKeyID), validClaims(now)),
		},
		{
			name:  "空の token は拒否される",
			token: "",
		},
		{
			name:  "JWT として parse できない token は拒否される",
			token: "not-a-jwt",
		},
		{
			name:  "kid header が無い token は拒否される",
			token: signWithoutKid(t, secret, validClaims(now)),
		},
		{
			name:  "未知の kid は拒否される",
			token: signWithKid(t, secret, "v999", validClaims(now)),
		},
		{
			name: "想定外の iss は拒否される",
			token: signWithKid(t, secret, string(DefaultKeyID), jwt.RegisteredClaims{
				Subject:   testPlayerID,
				Issuer:    "evil-issuer",
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			}),
		},
		{
			name:  "alg=none 攻撃は拒否される",
			token: noneAlgToken,
		},
		{
			name: "sub が空の token は拒否される",
			token: signWithKid(t, secret, string(DefaultKeyID), jwt.RegisteredClaims{
				Subject:   "",
				Issuer:    ExpectedIssuer,
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			}),
		},
	}

	v := NewVerifier(StaticHS256Resolver(secret, DefaultKeyID))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(tc.token)
			require.Error(t, err)
		})
	}
}

func TestStaticHS256Resolver_Match(t *testing.T) {
	secret := []byte(testSecretValue)
	resolver := StaticHS256Resolver(secret, DefaultKeyID)

	got, err := resolver(DefaultKeyID)
	require.NoError(t, err)
	assert.Equal(t, secret, got)
}

func TestStaticHS256Resolver_Mismatch(t *testing.T) {
	resolver := StaticHS256Resolver([]byte(testSecretValue), DefaultKeyID)
	_, err := resolver(KeyID("v999"))
	require.Error(t, err)
}
