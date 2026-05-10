package internalauth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func TestSigner_Issue(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxxx")
	fixedTime := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		opts      []Option
		playerID  string
		resolver  KeyResolver
		assertTok func(t *testing.T, parsed *jwt.Token, claims *jwt.RegisteredClaims)
		wantErr   bool
	}{
		{
			name:     "default options issue HS256 JWT with iss/sub/iat/exp/kid",
			opts:     []Option{WithClock(func() time.Time { return fixedTime })},
			playerID: "player-123",
			resolver: StaticHS256Resolver(secret, DefaultKeyID),
			assertTok: func(t *testing.T, parsed *jwt.Token, claims *jwt.RegisteredClaims) {
				t.Helper()
				if parsed.Method.Alg() != "HS256" {
					t.Errorf("alg = %s, want HS256", parsed.Method.Alg())
				}
				if got, want := parsed.Header["kid"], string(DefaultKeyID); got != want {
					t.Errorf("kid = %v, want %v", got, want)
				}
				if claims.Subject != "player-123" {
					t.Errorf("sub = %s, want player-123", claims.Subject)
				}
				if claims.Issuer != Issuer {
					t.Errorf("iss = %s, want %s", claims.Issuer, Issuer)
				}
				if !claims.IssuedAt.Time.Equal(fixedTime) {
					t.Errorf("iat = %v, want %v", claims.IssuedAt.Time, fixedTime)
				}
				if got := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); got != DefaultTTL {
					t.Errorf("exp - iat = %v, want %v", got, DefaultTTL)
				}
			},
		},
		{
			name:     "WithTTL overrides exp - iat",
			opts:     []Option{WithClock(func() time.Time { return fixedTime }), WithTTL(2 * time.Minute)},
			playerID: "player-123",
			resolver: StaticHS256Resolver(secret, DefaultKeyID),
			assertTok: func(t *testing.T, _ *jwt.Token, claims *jwt.RegisteredClaims) {
				t.Helper()
				if got := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); got != 2*time.Minute {
					t.Errorf("ttl = %v, want 2m", got)
				}
			},
		},
		{
			name:     "WithIssuer overrides iss",
			opts:     []Option{WithClock(func() time.Time { return fixedTime }), WithIssuer("other-issuer")},
			playerID: "player-123",
			resolver: StaticHS256Resolver(secret, DefaultKeyID),
			assertTok: func(t *testing.T, _ *jwt.Token, claims *jwt.RegisteredClaims) {
				t.Helper()
				if claims.Issuer != "other-issuer" {
					t.Errorf("iss = %s, want other-issuer", claims.Issuer)
				}
			},
		},
		{
			name:     "empty playerID returns error",
			playerID: "",
			resolver: StaticHS256Resolver(secret, DefaultKeyID),
			wantErr:  true,
		},
		{
			name:     "resolver error is propagated",
			playerID: "player-123",
			resolver: func(KeyID) ([]byte, error) { return nil, errors.New("boom") },
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := NewSigner(tc.resolver, DefaultKeyID, tc.opts...)
			token, err := signer.Issue(tc.playerID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			parsed, claims := parseAndAssertSignature(t, token, secret, fixedTime)
			if tc.assertTok != nil {
				tc.assertTok(t, parsed, claims)
			}
		})
	}
}

func TestStaticHS256Resolver_RejectsUnknownKeyID(t *testing.T) {
	secret := []byte("any-secret")
	resolver := StaticHS256Resolver(secret, DefaultKeyID)
	if _, err := resolver(KeyID("v2")); err == nil {
		t.Fatal("expected error for unknown kid, got nil")
	}
}

func parseAndAssertSignature(t *testing.T, token string, secret []byte, _ time.Time) (*jwt.Token, *jwt.RegisteredClaims) {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	// 固定時刻で発行した token を実時刻で検証すると iat/exp が衝突するため時刻検証を無効化する。
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsed, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return secret, nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("parsed token is not valid")
	}
	return parsed, claims
}
