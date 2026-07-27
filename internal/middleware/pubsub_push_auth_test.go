package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/api/idtoken"
)

// validPayload は idtoken.Validate が返す Payload のテスト用ダミーを組み立てる。
// 本物の Google 署名は使わず、googleIDTokenValidator 自身が行う issuer/email_verified/email の
// 検証ロジックだけを対象にする。
func validPayload(issuer, email string, emailVerified bool) *idtoken.Payload {
	return &idtoken.Payload{
		Issuer: issuer,
		Claims: map[string]interface{}{
			"email":          email,
			"email_verified": emailVerified,
		},
	}
}

func TestGoogleIDTokenValidator_Validate(t *testing.T) {
	t.Run("Google ID トークンの検証", func(t *testing.T) {
		t.Run("署名・有効期限の検証に失敗するとき、そのエラーを返す", func(t *testing.T) {
			wantErr := errors.New("idtoken: token expired: now=100, expires=50")
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return nil, wantErr
			}}

			_, err := v.Validate(context.Background(), "tok", "aud")

			require.Error(t, err)
			assert.ErrorIs(t, err, wantErr)
		})

		t.Run("audience の不一致で検証が失敗するとき、そのエラーを返す", func(t *testing.T) {
			wantErr := errors.New("idtoken: audience provided does not match aud claim in the JWT")
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return nil, wantErr
			}}

			_, err := v.Validate(context.Background(), "tok", "aud")

			require.Error(t, err)
			assert.ErrorIs(t, err, wantErr)
		})

		t.Run("署名が不正で検証が失敗するとき、そのエラーを返す", func(t *testing.T) {
			wantErr := errors.New("crypto/rsa: verification error")
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return nil, wantErr
			}}

			_, err := v.Validate(context.Background(), "tok", "aud")

			require.Error(t, err)
			assert.ErrorIs(t, err, wantErr)
		})

		t.Run("issuer が Google 以外のとき、エラーを返す", func(t *testing.T) {
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return validPayload("https://evil.example.com", "sa@test.iam.gserviceaccount.com", true), nil
			}}

			_, err := v.Validate(context.Background(), "tok", "aud")

			assert.Error(t, err)
		})

		t.Run("email_verified が false のとき、エラーを返す", func(t *testing.T) {
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return validPayload(googleOIDCIssuer, "sa@test.iam.gserviceaccount.com", false), nil
			}}

			_, err := v.Validate(context.Background(), "tok", "aud")

			assert.Error(t, err)
		})

		t.Run("email クレームが空のとき、エラーを返す", func(t *testing.T) {
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return validPayload(googleOIDCIssuer, "", true), nil
			}}

			_, err := v.Validate(context.Background(), "tok", "aud")

			assert.Error(t, err)
		})

		t.Run("すべての検証に成功するとき、email クレームを返す", func(t *testing.T) {
			v := &googleIDTokenValidator{validate: func(context.Context, string, string) (*idtoken.Payload, error) {
				return validPayload(googleOIDCIssuer, "sa@test.iam.gserviceaccount.com", true), nil
			}}

			email, err := v.Validate(context.Background(), "tok", "aud")

			require.NoError(t, err)
			assert.Equal(t, "sa@test.iam.gserviceaccount.com", email)
		})

		t.Run("audience をそのまま検証関数へ渡す", func(t *testing.T) {
			var gotAudience string
			v := &googleIDTokenValidator{validate: func(_ context.Context, _ string, audience string) (*idtoken.Payload, error) {
				gotAudience = audience
				return validPayload(googleOIDCIssuer, "sa@test.iam.gserviceaccount.com", true), nil
			}}

			_, err := v.Validate(context.Background(), "tok", "https://gateway.example.com/internal/v1/pubsub/match-made")

			require.NoError(t, err)
			assert.Equal(t, "https://gateway.example.com/internal/v1/pubsub/match-made", gotAudience)
		})
	})
}

// fakePushTokenValidator は PubSubPushTokenValidator のテスト用実装。
type fakePushTokenValidator struct {
	email string
	err   error
}

func (f *fakePushTokenValidator) Validate(context.Context, string, string) (string, error) {
	return f.email, f.err
}

var _ PubSubPushTokenValidator = (*fakePushTokenValidator)(nil)

const testPushServiceAccountEmail = "pubsub-push@test-project.iam.gserviceaccount.com"

func newPushAuthTestRouter(validator PubSubPushTokenValidator) *gin.Engine {
	r := gin.New()
	r.Use(UsePubSubPushAuth(validator, testPushServiceAccountEmail, "https://gateway.example.com/internal/v1/pubsub/match-made"))
	r.POST("/push", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestUsePubSubPushAuth(t *testing.T) {
	t.Run("push リクエストの OIDC 認証", func(t *testing.T) {
		t.Run("Authorization ヘッダが無いとき、401 を返し下流に到達しない", func(t *testing.T) {
			r := newPushAuthTestRouter(&fakePushTokenValidator{email: testPushServiceAccountEmail})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/push", nil))

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing authorization header")
		})

		t.Run("Authorization ヘッダが Bearer 形式でないとき、401 を返し下流に到達しない", func(t *testing.T) {
			r := newPushAuthTestRouter(&fakePushTokenValidator{email: testPushServiceAccountEmail})
			req := httptest.NewRequest(http.MethodPost, "/push", nil)
			req.Header.Set("Authorization", "token-without-bearer-prefix")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "invalid authorization format")
		})

		t.Run("トークンの検証に失敗するとき、401 を返し下流に到達しない", func(t *testing.T) {
			r := newPushAuthTestRouter(&fakePushTokenValidator{err: errors.New("boom")})
			req := httptest.NewRequest(http.MethodPost, "/push", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "invalid token")
		})

		t.Run("検証には成功するが期待するサービスアカウントと一致しないとき、401 を返し下流に到達しない", func(t *testing.T) {
			r := newPushAuthTestRouter(&fakePushTokenValidator{email: "someone-else@test-project.iam.gserviceaccount.com"})
			req := httptest.NewRequest(http.MethodPost, "/push", nil)
			req.Header.Set("Authorization", "Bearer valid-but-wrong-sa")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "unexpected service account")
		})

		t.Run("期待するサービスアカウントで検証に成功するとき、下流に到達する", func(t *testing.T) {
			r := newPushAuthTestRouter(&fakePushTokenValidator{email: testPushServiceAccountEmail})
			req := httptest.NewRequest(http.MethodPost, "/push", nil)
			req.Header.Set("Authorization", "Bearer valid-token")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	})
}
