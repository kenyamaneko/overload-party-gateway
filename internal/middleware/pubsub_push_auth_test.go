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

func TestGoogleIDTokenValidatorValidate(t *testing.T) {
	t.Run("[転送配信]PubSub pushトークンの正当性判定", func(t *testing.T) {
		validClaims := map[string]interface{}{"email_verified": true, "email": "sa@example.com"}

		errorCases := []struct {
			name          string
			validateFn    idTokenValidateFunc
			wantErrSubstr string
		}{
			{
				name: "トークン自体の署名・有効期限などの基礎検証が失敗するとき、検証は失敗と判定される",
				validateFn: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
					return nil, errors.New("signature invalid")
				},
				wantErrSubstr: "signature invalid",
			},
			{
				name: "基礎検証は通るが、発行者の情報がGoogleの発行者情報と一致しないとき、検証は失敗と判定される",
				validateFn: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
					return &idtoken.Payload{Issuer: "https://evil.example", Claims: validClaims}, nil
				},
				wantErrSubstr: "unexpected issuer",
			},
			{
				name: "発行者情報は一致するが、メールアドレスが検証済みであることを示すクレームが真でないとき(クレーム自体が存在しない場合)、検証は失敗と判定される",
				validateFn: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
					return &idtoken.Payload{Issuer: googleOIDCIssuer, Claims: map[string]interface{}{"email": "sa@example.com"}}, nil
				},
				wantErrSubstr: "email not verified",
			},
			{
				name: "発行者情報は一致するが、メールアドレスが検証済みであることを示すクレームが真でないとき(クレームがfalseの場合)、検証は失敗と判定される",
				validateFn: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
					return &idtoken.Payload{Issuer: googleOIDCIssuer, Claims: map[string]interface{}{"email_verified": false, "email": "sa@example.com"}}, nil
				},
				wantErrSubstr: "email not verified",
			},
			{
				name: "発行者情報とメール検証済みクレームは満たすが、メールアドレスのクレームが存在しないとき、検証は失敗と判定される",
				validateFn: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
					return &idtoken.Payload{Issuer: googleOIDCIssuer, Claims: map[string]interface{}{"email_verified": true}}, nil
				},
				wantErrSubstr: "email claim missing",
			},
			{
				name: "発行者情報とメール検証済みクレームは満たすが、メールアドレスのクレームが空のとき、検証は失敗と判定される",
				validateFn: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
					return &idtoken.Payload{Issuer: googleOIDCIssuer, Claims: map[string]interface{}{"email_verified": true, "email": ""}}, nil
				},
				wantErrSubstr: "email claim missing",
			},
		}

		for _, tc := range errorCases {
			t.Run(tc.name, func(t *testing.T) {
				v := &googleIDTokenValidator{validate: tc.validateFn}

				email, err := v.Validate(context.Background(), "id-token", "audience")

				require.Error(t, err)
				assert.ErrorContains(t, err, tc.wantErrSubstr)
				assert.Equal(t, "", email)
			})
		}

		t.Run("発行者情報・メール検証済み・メールアドレスのすべての条件を満たすとき、検証は成功し、そのメールアドレスが判定結果として返る", func(t *testing.T) {
			v := &googleIDTokenValidator{validate: func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
				return &idtoken.Payload{Issuer: googleOIDCIssuer, Claims: validClaims}, nil
			}}

			email, err := v.Validate(context.Background(), "id-token", "audience")

			require.NoError(t, err)
			assert.Equal(t, "sa@example.com", email)
		})
	})
}

func TestUsePubSubPushAuth(t *testing.T) {
	t.Run("[転送配信]PubSub pushリクエストの認可", func(t *testing.T) {
		const expectedEmail = "push-sa@example.com"
		const audience = "https://gateway.example.com/pubsub/push"

		t.Run("リクエストに認証ヘッダーが無いとき、401で拒否される", func(t *testing.T) {
			reached := false
			r := gin.New()
			r.POST("/push", UsePubSubPushAuth(&stubPubSubPushTokenValidator{}, expectedEmail, audience), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/push", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing authorization header")
			assert.False(t, reached)
		})

		cases := []struct {
			name       string
			authHeader string
			validator  *stubPubSubPushTokenValidator
			wantBody   string
		}{
			{
				name:       `認証ヘッダーが"Bearer "で始まっていないとき、401で拒否される(不正な形式)`,
				authHeader: "Token xyz",
				validator:  &stubPubSubPushTokenValidator{},
				wantBody:   "invalid authorization format",
			},
			{
				name:       "トークンの正当性判定が失敗したとき、401で拒否される",
				authHeader: "Bearer id-token",
				validator: &stubPubSubPushTokenValidator{
					validate: func(ctx context.Context, idToken, audience string) (string, error) {
						return "", errors.New("token invalid")
					},
				},
				wantBody: "invalid token",
			},
			{
				name:       "トークンの正当性判定は成功するが、判定されたメールアドレスが想定するサービスアカウントのメールアドレスと一致しないとき、401で拒否される",
				authHeader: "Bearer id-token",
				validator: &stubPubSubPushTokenValidator{
					validate: func(ctx context.Context, idToken, audience string) (string, error) {
						return "other-sa@example.com", nil
					},
				},
				wantBody: "unexpected service account",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				r := gin.New()
				r.POST("/push", UsePubSubPushAuth(tc.validator, expectedEmail, audience), func(c *gin.Context) {
					reached = true
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodPost, "/push", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code)
				assert.Contains(t, w.Body.String(), tc.wantBody)
				assert.False(t, reached)
			})
		}

		t.Run("トークンの正当性判定が成功し、判定されたメールアドレスが想定するサービスアカウントのメールアドレスと一致するとき、後続の処理に進む", func(t *testing.T) {
			reached := false
			validator := &stubPubSubPushTokenValidator{
				validate: func(ctx context.Context, idToken, audience string) (string, error) {
					return expectedEmail, nil
				},
			}
			r := gin.New()
			r.POST("/push", UsePubSubPushAuth(validator, expectedEmail, audience), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/push", nil)
			req.Header.Set("Authorization", "Bearer id-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, reached)
		})
	})
}
