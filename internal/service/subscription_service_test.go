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
	shopRepo   *repository.MockShopRepository
	playerRepo *repository.MockPlayerRepository
}

func newTestSubscriptionService() *testSubEnv {
	shopRepo := repository.NewMockShopRepository()
	playerRepo := repository.NewMockPlayerRepository()
	svc := NewSubscriptionService(shopRepo, playerRepo)
	return &testSubEnv{svc: svc, shopRepo: shopRepo, playerRepo: playerRepo}
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
	_ = env.shopRepo.CreateSubscription(context.Background(), sub)
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

func TestHandleAppleNotification_Renewal(t *testing.T) {
	env := newTestSubscriptionService()
	sub := createTestSubscription(env, "p1", "apple-token-1")

	newExpiry := time.Now().Add(60 * 24 * time.Hour).UnixMilli()

	txnInfo := buildAppleJWS(map[string]interface{}{
		"originalTransactionId": sub.PurchaseToken,
		"expiresDate":           newExpiry,
	})

	notifPayload := buildAppleJWS(map[string]interface{}{
		"notificationType": "DID_RENEW",
		"data": map[string]interface{}{
			"signedTransactionInfo": txnInfo,
		},
	})

	err := env.svc.HandleAppleNotification(context.Background(), notifPayload)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	require.NotNil(t, updatedSub)
	assert.Equal(t, model.SubscriptionStatusActive, updatedSub.Status)

	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	require.NotNil(t, p)
	assert.True(t, p.IsPremium)
}

func TestHandleAppleNotification_DeactivatesPremium(t *testing.T) {
	tests := []struct {
		name           string
		notifType      string
		token          string
		expectedStatus string
	}{
		{
			name:           "Expired",
			notifType:      "EXPIRED",
			token:          "apple-token-2",
			expectedStatus: model.SubscriptionStatusExpired,
		},
		{
			name:           "GracePeriodExpired",
			notifType:      "GRACE_PERIOD_EXPIRED",
			token:          "apple-token-gpe",
			expectedStatus: model.SubscriptionStatusExpired,
		},
		{
			name:           "Revoke",
			notifType:      "REVOKE",
			token:          "apple-token-revoke",
			expectedStatus: model.SubscriptionStatusRevoked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()
			sub := createTestSubscription(env, "p1", tt.token)

			txnInfo := buildAppleJWS(map[string]interface{}{
				"originalTransactionId": sub.PurchaseToken,
				"expiresDate":           time.Now().UnixMilli(),
			})

			notifPayload := buildAppleJWS(map[string]interface{}{
				"notificationType": tt.notifType,
				"data": map[string]interface{}{
					"signedTransactionInfo": txnInfo,
				},
			})

			err := env.svc.HandleAppleNotification(context.Background(), notifPayload)
			require.NoError(t, err)

			updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
			require.NotNil(t, updatedSub)
			assert.Equal(t, tt.expectedStatus, updatedSub.Status)

			p, _ := env.playerRepo.FindByID(context.Background(), "p1")
			require.NotNil(t, p)
			assert.False(t, p.IsPremium)
		})
	}
}

func TestHandleAppleNotification_NoOpUnchanged(t *testing.T) {
	tests := []struct {
		name      string
		notifType string
		subtype   string
		token     string
	}{
		{
			name:      "UnknownType",
			notifType: "UNKNOWN_TYPE",
			token:     "apple-token-unknown",
		},
		{
			name:      "AutoRenewEnabled",
			notifType: "DID_CHANGE_RENEWAL_STATUS",
			subtype:   "AUTO_RENEW_ENABLED",
			token:     "apple-token-are",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()
			sub := createTestSubscription(env, "p1", tt.token)

			txnPayload := map[string]interface{}{
				"originalTransactionId": sub.PurchaseToken,
				"expiresDate":           sub.CurrentPeriodEnd.UnixMilli(),
			}
			if tt.name == "UnknownType" {
				txnPayload["expiresDate"] = time.Now().UnixMilli()
			}
			txnInfo := buildAppleJWS(txnPayload)

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

			updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
			assert.Equal(t, model.SubscriptionStatusActive, updatedSub.Status)

			p, _ := env.playerRepo.FindByID(context.Background(), "p1")
			require.NotNil(t, p)
			assert.True(t, p.IsPremium)
		})
	}
}

func TestHandleAppleNotification_AutoRenewDisabled(t *testing.T) {
	env := newTestSubscriptionService()
	sub := createTestSubscription(env, "p1", "apple-token-ard")

	txnInfo := buildAppleJWS(map[string]interface{}{
		"originalTransactionId": sub.PurchaseToken,
		"expiresDate":           sub.CurrentPeriodEnd.UnixMilli(),
	})

	notifPayload := buildAppleJWS(map[string]interface{}{
		"notificationType": "DID_CHANGE_RENEWAL_STATUS",
		"subtype":          "AUTO_RENEW_DISABLED",
		"data": map[string]interface{}{
			"signedTransactionInfo": txnInfo,
		},
	})

	err := env.svc.HandleAppleNotification(context.Background(), notifPayload)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	require.NotNil(t, updatedSub)
	assert.Equal(t, model.SubscriptionStatusCancelled, updatedSub.Status)

	// Premium should remain active until current_period_end
	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	require.NotNil(t, p)
	assert.True(t, p.IsPremium)
}

func TestHandleAppleNotification_AlreadyExpired(t *testing.T) {
	env := newTestSubscriptionService()
	sub := createTestSubscription(env, "p1", "apple-token-already-exp")

	// Pre-expire the subscription
	sub.Status = model.SubscriptionStatusExpired
	_ = env.shopRepo.UpdateSubscription(context.Background(), sub)
	_ = env.playerRepo.UpdatePremium(context.Background(), "p1", false, nil)

	txnInfo := buildAppleJWS(map[string]interface{}{
		"originalTransactionId": sub.PurchaseToken,
		"expiresDate":           time.Now().UnixMilli(),
	})

	notifPayload := buildAppleJWS(map[string]interface{}{
		"notificationType": "EXPIRED",
		"data": map[string]interface{}{
			"signedTransactionInfo": txnInfo,
		},
	})

	err := env.svc.HandleAppleNotification(context.Background(), notifPayload)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	assert.Equal(t, model.SubscriptionStatusExpired, updatedSub.Status)

	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	require.NotNil(t, p)
	assert.False(t, p.IsPremium)
}

func TestHandleGoogleNotification_Renewed(t *testing.T) {
	env := newTestSubscriptionService()
	_ = createTestSubscription(env, "p1", "google-token-1")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubRenewed,
			"purchaseToken":    "google-token-1",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := env.svc.HandleGoogleNotification(context.Background(), msg)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), "google-token-1")
	assert.Equal(t, model.SubscriptionStatusActive, updatedSub.Status)
}

func TestHandleGoogleNotification_DeactivatesPremium(t *testing.T) {
	tests := []struct {
		name           string
		notifType      int
		token          string
		expectedStatus string
	}{
		{
			name:           "Revoked",
			notifType:      googleSubRevoked,
			token:          "google-token-2",
			expectedStatus: model.SubscriptionStatusRevoked,
		},
		{
			name:           "Expired",
			notifType:      googleSubExpired,
			token:          "google-token-expire",
			expectedStatus: model.SubscriptionStatusExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestSubscriptionService()
			_ = createTestSubscription(env, "p1", tt.token)

			data, _ := json.Marshal(map[string]interface{}{
				"subscriptionNotification": map[string]interface{}{
					"notificationType": tt.notifType,
					"purchaseToken":    tt.token,
					"subscriptionId":   "premium_monthly",
				},
			})

			msg := GoogleRTDNMessage{}
			msg.Message.Data = base64.StdEncoding.EncodeToString(data)

			err := env.svc.HandleGoogleNotification(context.Background(), msg)
			require.NoError(t, err)

			updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), tt.token)
			require.NotNil(t, updatedSub)
			assert.Equal(t, tt.expectedStatus, updatedSub.Status)

			p, _ := env.playerRepo.FindByID(context.Background(), "p1")
			require.NotNil(t, p)
			assert.False(t, p.IsPremium)
		})
	}
}

func TestHandleGoogleNotification_Canceled(t *testing.T) {
	env := newTestSubscriptionService()
	_ = createTestSubscription(env, "p1", "google-token-cancel")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubCanceled,
			"purchaseToken":    "google-token-cancel",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := env.svc.HandleGoogleNotification(context.Background(), msg)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), "google-token-cancel")
	require.NotNil(t, updatedSub)
	assert.Equal(t, model.SubscriptionStatusCancelled, updatedSub.Status)

	// Premium should remain active until current_period_end (not revoked immediately)
	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	require.NotNil(t, p)
	assert.True(t, p.IsPremium)
}

func TestHandleGoogleNotification_Recovered(t *testing.T) {
	env := newTestSubscriptionService()
	sub := createTestSubscription(env, "p1", "google-token-recover")

	// Simulate a previously expired subscription
	sub.Status = model.SubscriptionStatusExpired
	_ = env.shopRepo.UpdateSubscription(context.Background(), sub)
	_ = env.playerRepo.UpdatePremium(context.Background(), "p1", false, nil)

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubRecovered,
			"purchaseToken":    "google-token-recover",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := env.svc.HandleGoogleNotification(context.Background(), msg)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), "google-token-recover")
	require.NotNil(t, updatedSub)
	assert.Equal(t, model.SubscriptionStatusActive, updatedSub.Status)

	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	require.NotNil(t, p)
	assert.True(t, p.IsPremium)
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

func TestHandleGoogleNotification_UnhandledType(t *testing.T) {
	env := newTestSubscriptionService()
	sub := createTestSubscription(env, "p1", "google-token-unhandled")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": 99, // not in switch cases
			"purchaseToken":    sub.PurchaseToken,
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := env.svc.HandleGoogleNotification(context.Background(), msg)
	require.NoError(t, err)

	updatedSub, _ := env.shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	assert.Equal(t, model.SubscriptionStatusActive, updatedSub.Status)
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
