package internalauth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSigner_Issue_Success(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxxx")
	fixedTime := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		opts      []Option
		assertTok func(t *testing.T, parsed *jwt.Token, claims *jwt.RegisteredClaims)
	}{
		{
			name: "デフォルトのオプションで HS256 JWT を発行し iss/sub/iat/exp/kid が揃う",
			opts: []Option{WithClock(func() time.Time { return fixedTime })},
			assertTok: func(t *testing.T, parsed *jwt.Token, claims *jwt.RegisteredClaims) {
				t.Helper()
				assert.Equal(t, "HS256", parsed.Method.Alg())
				assert.Equal(t, string(DefaultKeyID), parsed.Header["kid"])
				assert.Equal(t, "player-123", claims.Subject)
				assert.Equal(t, Issuer, claims.Issuer)
				assert.True(t, claims.IssuedAt.Equal(fixedTime))
				assert.Equal(t, DefaultTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
			},
		},
		{
			name: "WithTTL で exp-iat を上書きできる",
			opts: []Option{WithClock(func() time.Time { return fixedTime }), WithTTL(2 * time.Minute)},
			assertTok: func(t *testing.T, _ *jwt.Token, claims *jwt.RegisteredClaims) {
				t.Helper()
				assert.Equal(t, 2*time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
			},
		},
		{
			name: "WithIssuer で iss を上書きできる",
			opts: []Option{WithClock(func() time.Time { return fixedTime }), WithIssuer("other-issuer")},
			assertTok: func(t *testing.T, _ *jwt.Token, claims *jwt.RegisteredClaims) {
				t.Helper()
				assert.Equal(t, "other-issuer", claims.Issuer)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := NewSigner(StaticHS256Resolver(secret, DefaultKeyID), DefaultKeyID, tc.opts...)
			token, err := signer.Issue("player-123")
			require.NoError(t, err)
			parsed, claims := parseAndAssertSignature(t, token, secret)
			tc.assertTok(t, parsed, claims)
		})
	}
}

func TestSigner_Issue_Error(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxxx")

	cases := []struct {
		name     string
		playerID string
		resolver KeyResolver
	}{
		{
			name:     "playerID が空のとき error を返す",
			playerID: "",
			resolver: StaticHS256Resolver(secret, DefaultKeyID),
		},
		{
			name:     "resolver の error が伝搬する",
			playerID: "player-123",
			resolver: func(KeyID) ([]byte, error) { return nil, errors.New("boom") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := NewSigner(tc.resolver, DefaultKeyID)
			_, err := signer.Issue(tc.playerID)
			require.Error(t, err)
		})
	}
}

func TestStaticHS256Resolver_RejectsUnknownKeyID(t *testing.T) {
	resolver := StaticHS256Resolver([]byte("any-secret"), DefaultKeyID)
	_, err := resolver(KeyID("v2"))
	require.Error(t, err)
}

func parseAndAssertSignature(t *testing.T, token string, secret []byte) (*jwt.Token, *jwt.RegisteredClaims) {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	// 固定時刻で発行した token を実時刻で検証すると iat/exp が衝突するため時刻検証を無効化する。
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsed, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return secret, nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	return parsed, claims
}
