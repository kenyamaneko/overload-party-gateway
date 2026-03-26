package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSubEnv struct {
	svc        *SubscriptionService
	subRepo    *repository.MockSubscriptionRepository
	playerRepo *repository.MockPlayerRepository
}

func newTestSubscriptionService() *testSubEnv {
	subRepo := repository.NewMockSubscriptionRepository()
	playerRepo := repository.NewMockPlayerRepository()
	svc := NewSubscriptionService(subRepo, playerRepo, &repository.MockTxRunner{})
	return &testSubEnv{svc: svc, subRepo: subRepo, playerRepo: playerRepo}
}

func createTestSubscription(env *testSubEnv, playerID, purchaseToken string) *model.Subscription {
	now := time.Now()
	periodEnd := now.Add(30 * 24 * time.Hour)

	// Ensure the player exists in the mock player repo.
	_ = env.playerRepo.Create(context.Background(), &model.Player{
		PlayerID:         playerID,
		IsPremium:        true,
		PremiumExpiresAt: &periodEnd,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, &model.PlayerDailyBattle{PlayerID: playerID})

	sub := &model.Subscription{
		PlayerID:           playerID,
		ProductID:          "premium_monthly",
		Platform:           "ios",
		PurchaseToken:      purchaseToken,
		Status:             model.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   periodEnd,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = env.subRepo.CreateSubscription(context.Background(), sub)
	return sub
}

// buildAppleJWS builds a fake JWS (header.payload.signature) for testing.
func buildAppleJWS(payload interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256"}`))
	data, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(data)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + body + "." + sig
}

func TestHandleAppleNotification(t *testing.T) {
	tests := []struct {
		name           string
		notifType      string
		subtype        string
		preExpire      bool
		expectedStatus string
		expectedPremium bool
	}{
		{"Renewal", "DID_RENEW", "", false, model.SubscriptionStatusActive, true},
		{"Expired", "EXPIRED", "", false, model.SubscriptionStatusExpired, false},
		{"GracePeriodExpired", "GRACE_PERIOD_EXPIRED", "", false, model.SubscriptionStatusExpired, false},
		{"Revoke", "REVOKE", "", false, model.SubscriptionStatusRevoked, false},
		{"UnknownType", "UNKNOWN_TYPE", "", false, model.SubscriptionStatusActive, true},
		{"AutoRenewEnabled", "DID_CHANGE_RENEWAL_STATUS", "AUTO_RENEW_ENABLED", false, model.SubscriptionStatusActive, true},
		{"AutoRenewDisabled", "DID_CHANGE_RENEWAL_STATUS", "AUTO_RENEW_DISABLED", false, model.SubscriptionStatusCancelled, true},
		{"AlreadyExpired", "EXPIRED", "", true, model.SubscriptionStatusExpired, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()
			sub := createTestSubscription(env, "p1", "apple-token-"+tt.name)

			if tt.preExpire {
				sub.Status = model.SubscriptionStatusExpired
				_ = env.subRepo.UpdateSubscription(context.Background(), sub)
				_ = env.playerRepo.UpdatePremium(context.Background(), "p1", false, nil)
			}

			txnInfo := buildAppleJWS(map[string]interface{}{
				"originalTransactionId": sub.PurchaseToken,
				"expiresDate":           time.Now().UnixMilli(),
			})

			notifData := map[string]interface{}{
				"notificationType": tt.notifType,
				"data": map[string]interface{}{
					"signedTransactionInfo": txnInfo,
				},
			}
			if tt.subtype != "" {
				notifData["subtype"] = tt.subtype
			}
			notifPayload := buildAppleJWS(notifData)

			err := env.svc.HandleAppleNotification(context.Background(), notifPayload)
			require.NoError(t, err)

			updatedSub, _ := env.subRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
			require.NotNil(t, updatedSub)
			assert.Equal(t, tt.expectedStatus, updatedSub.Status)

			p, _ := env.playerRepo.FindByID(context.Background(), "p1")
			require.NotNil(t, p)
			assert.Equal(t, tt.expectedPremium, p.IsPremium)
		})
	}
}

func TestHandleGoogleNotification(t *testing.T) {
	tests := []struct {
		name            string
		notifType       int
		preExpire       bool
		expectedStatus  string
		expectedPremium bool
	}{
		{"Renewed", googleSubRenewed, false, model.SubscriptionStatusActive, true},
		{"Revoked", googleSubRevoked, false, model.SubscriptionStatusRevoked, false},
		{"Expired", googleSubExpired, false, model.SubscriptionStatusExpired, false},
		{"Canceled", googleSubCanceled, false, model.SubscriptionStatusCancelled, true},
		{"Recovered", googleSubRecovered, true, model.SubscriptionStatusActive, true},
		{"UnhandledType", 99, false, model.SubscriptionStatusActive, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()
			sub := createTestSubscription(env, "p1", "google-token-"+tt.name)

			if tt.preExpire {
				sub.Status = model.SubscriptionStatusExpired
				_ = env.subRepo.UpdateSubscription(context.Background(), sub)
				_ = env.playerRepo.UpdatePremium(context.Background(), "p1", false, nil)
			}

			data, _ := json.Marshal(map[string]interface{}{
				"subscriptionNotification": map[string]interface{}{
					"notificationType": tt.notifType,
					"purchaseToken":    sub.PurchaseToken,
					"subscriptionId":   "premium_monthly",
				},
			})

			msg := GoogleRTDNMessage{}
			msg.Message.Data = base64.StdEncoding.EncodeToString(data)

			err := env.svc.HandleGoogleNotification(context.Background(), msg)
			require.NoError(t, err)

			updatedSub, _ := env.subRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
			require.NotNil(t, updatedSub)
			assert.Equal(t, tt.expectedStatus, updatedSub.Status)

			p, _ := env.playerRepo.FindByID(context.Background(), "p1")
			require.NotNil(t, p)
			assert.Equal(t, tt.expectedPremium, p.IsPremium)
		})
	}
}

func TestHandleGoogleNotification_NonSubscription(t *testing.T) {
	env := newTestSubscriptionService()

	data, _ := json.Marshal(map[string]interface{}{
		"voidedPurchaseNotification": map[string]interface{}{
			"orderId": "GPA.1234",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := env.svc.HandleGoogleNotification(context.Background(), msg)
	require.NoError(t, err)
}

func TestHandleNotification_SubscriptionNotFound(t *testing.T) {
	tests := []struct {
		name     string
		platform string
	}{
		{name: "Apple", platform: "apple"},
		{name: "Google", platform: "google"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()

			var err error
			if tt.platform == "apple" {
				txnInfo := buildAppleJWS(map[string]interface{}{
					"originalTransactionId": "nonexistent-token",
					"expiresDate":           time.Now().UnixMilli(),
				})

				notifPayload := buildAppleJWS(map[string]interface{}{
					"notificationType": "EXPIRED",
					"data": map[string]interface{}{
						"signedTransactionInfo": txnInfo,
					},
				})

				err = env.svc.HandleAppleNotification(context.Background(), notifPayload)
			} else {
				data, _ := json.Marshal(map[string]interface{}{
					"subscriptionNotification": map[string]interface{}{
						"notificationType": googleSubExpired,
						"purchaseToken":    "nonexistent-google-token",
						"subscriptionId":   "premium_monthly",
					},
				})

				msg := GoogleRTDNMessage{}
				msg.Message.Data = base64.StdEncoding.EncodeToString(data)

				err = env.svc.HandleGoogleNotification(context.Background(), msg)
			}

			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "subscription not found"))
		})
	}
}

func TestHandleNotification_DecodeErrors(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		input       string
		errContains string
	}{
		{
			name:        "Apple/InvalidJWS",
			platform:    "apple",
			input:       "not-a-valid-jws",
			errContains: "decode notification",
		},
		{
			name:        "Apple/InvalidTransactionInfoJWS",
			platform:    "apple",
			errContains: "decode transaction info",
		},
		{
			name:        "Google/InvalidBase64",
			platform:    "google",
			input:       "!!! not base64 !!!",
			errContains: "decode RTDN data",
		},
		{
			name:        "Google/InvalidJSON",
			platform:    "google",
			input:       base64.StdEncoding.EncodeToString([]byte("not valid json {{{")),
			errContains: "unmarshal RTDN data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()

			var err error
			if tt.platform == "apple" {
				input := tt.input
				if input == "" {
					input = buildAppleJWS(map[string]interface{}{
						"notificationType": "EXPIRED",
						"data": map[string]interface{}{
							"signedTransactionInfo": "not-a-valid-jws",
						},
					})
				}
				err = env.svc.HandleAppleNotification(context.Background(), input)
			} else {
				msg := GoogleRTDNMessage{}
				msg.Message.Data = tt.input
				err = env.svc.HandleGoogleNotification(context.Background(), msg)
			}

			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tt.errContains))
		})
	}
}
