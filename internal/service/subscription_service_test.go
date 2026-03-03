package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kenyamaneko/overload-party-common/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

func newTestSubscriptionService() (*SubscriptionService, *repository.MockShopRepository) {
	shopRepo := repository.NewMockShopRepository()
	svc := NewSubscriptionService(shopRepo)
	return svc, shopRepo
}

func createTestSubscription(shopRepo *repository.MockShopRepository, playerID, purchaseToken string) *model.Subscription {
	sub := &model.Subscription{
		PlayerID:           playerID,
		ProductID:          "premium_monthly",
		Platform:           "ios",
		PurchaseToken:      purchaseToken,
		Status:             model.SubscriptionStatusActive,
		CurrentPeriodStart: time.Now(),
		CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	_ = shopRepo.CreateSubscription(context.Background(), sub)
	_ = shopRepo.UpdatePlayerPremium(context.Background(), playerID, true, &sub.CurrentPeriodEnd)
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
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "apple-token-1")

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

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify subscription is still active
	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusActive {
		t.Errorf("expected status active, got %s", updatedSub.Status)
	}

	// Verify player is still premium
	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || !pd.IsPremium {
		t.Error("expected player to be premium")
	}
}

func TestHandleAppleNotification_Expired(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "apple-token-2")

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

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	if updatedSub.Status != model.SubscriptionStatusExpired {
		t.Errorf("expected status expired, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || pd.IsPremium {
		t.Error("expected player to not be premium")
	}
}

func TestHandleGoogleNotification_Renewed(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	_ = createTestSubscription(shopRepo, "p1", "google-token-1")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubRenewed,
			"purchaseToken":    "google-token-1",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), "google-token-1")
	if updatedSub.Status != model.SubscriptionStatusActive {
		t.Errorf("expected status active, got %s", updatedSub.Status)
	}
}

func TestHandleGoogleNotification_Revoked(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	_ = createTestSubscription(shopRepo, "p1", "google-token-2")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubRevoked,
			"purchaseToken":    "google-token-2",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), "google-token-2")
	if updatedSub.Status != model.SubscriptionStatusRevoked {
		t.Errorf("expected status revoked, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || pd.IsPremium {
		t.Error("expected player to not be premium")
	}
}

// --- Apple GRACE_PERIOD_EXPIRED ---

func TestHandleAppleNotification_GracePeriodExpired(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "apple-token-gpe")

	txnInfo := buildAppleJWS(map[string]interface{}{
		"originalTransactionId": sub.PurchaseToken,
		"expiresDate":           time.Now().UnixMilli(),
	})

	notifPayload := buildAppleJWS(map[string]interface{}{
		"notificationType": "GRACE_PERIOD_EXPIRED",
		"data": map[string]interface{}{
			"signedTransactionInfo": txnInfo,
		},
	})

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusExpired {
		t.Errorf("expected status expired, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || pd.IsPremium {
		t.Error("expected player to not be premium after grace period expired")
	}
}

// --- Apple REVOKE ---

func TestHandleAppleNotification_Revoke(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "apple-token-revoke")

	txnInfo := buildAppleJWS(map[string]interface{}{
		"originalTransactionId": sub.PurchaseToken,
		"expiresDate":           time.Now().UnixMilli(),
	})

	notifPayload := buildAppleJWS(map[string]interface{}{
		"notificationType": "REVOKE",
		"data": map[string]interface{}{
			"signedTransactionInfo": txnInfo,
		},
	})

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusRevoked {
		t.Errorf("expected status revoked, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || pd.IsPremium {
		t.Error("expected player to not be premium after revoke")
	}
}

// --- Apple DID_CHANGE_RENEWAL_STATUS (AUTO_RENEW_DISABLED) ---

func TestHandleAppleNotification_AutoRenewDisabled(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "apple-token-ard")

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

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusCancelled {
		t.Errorf("expected status cancelled, got %s", updatedSub.Status)
	}

	// Premium should remain active until current_period_end
	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || !pd.IsPremium {
		t.Error("expected player to remain premium until period end")
	}
}

// --- Google canceled ---

func TestHandleGoogleNotification_Canceled(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	_ = createTestSubscription(shopRepo, "p1", "google-token-cancel")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubCanceled,
			"purchaseToken":    "google-token-cancel",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), "google-token-cancel")
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusCancelled {
		t.Errorf("expected status cancelled, got %s", updatedSub.Status)
	}

	// Premium should remain active until current_period_end (not revoked immediately)
	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || !pd.IsPremium {
		t.Error("expected player to remain premium until period end after cancel")
	}
}

// --- Google expired ---

func TestHandleGoogleNotification_Expired(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	_ = createTestSubscription(shopRepo, "p1", "google-token-expire")

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubExpired,
			"purchaseToken":    "google-token-expire",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), "google-token-expire")
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusExpired {
		t.Errorf("expected status expired, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || pd.IsPremium {
		t.Error("expected player to not be premium after expiration")
	}
}

// --- Google recovered ---

func TestHandleGoogleNotification_Recovered(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "google-token-recover")

	// Simulate a previously expired subscription
	sub.Status = model.SubscriptionStatusExpired
	_ = shopRepo.UpdateSubscription(context.Background(), sub)
	_ = shopRepo.UpdatePlayerPremium(context.Background(), "p1", false, nil)

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubRecovered,
			"purchaseToken":    "google-token-recover",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), "google-token-recover")
	if updatedSub == nil {
		t.Fatal("subscription not found")
	}
	if updatedSub.Status != model.SubscriptionStatusActive {
		t.Errorf("expected status active, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || !pd.IsPremium {
		t.Error("expected player to be premium after recovery")
	}
}

// --- Edge case: subscription not found ---

func TestHandleAppleNotification_SubscriptionNotFound(t *testing.T) {
	svc, _ := newTestSubscriptionService()

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

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err == nil {
		t.Fatal("expected error for nonexistent subscription, got nil")
	}
	if !strings.Contains(err.Error(), "subscription not found") {
		t.Errorf("expected 'subscription not found' error, got: %v", err)
	}
}

func TestHandleGoogleNotification_SubscriptionNotFound(t *testing.T) {
	svc, _ := newTestSubscriptionService()

	data, _ := json.Marshal(map[string]interface{}{
		"subscriptionNotification": map[string]interface{}{
			"notificationType": googleSubExpired,
			"purchaseToken":    "nonexistent-google-token",
			"subscriptionId":   "premium_monthly",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for nonexistent subscription, got nil")
	}
	if !strings.Contains(err.Error(), "subscription not found") {
		t.Errorf("expected 'subscription not found' error, got: %v", err)
	}
}

// --- Edge case: Google non-subscription notification (should be silently ignored) ---

func TestHandleGoogleNotification_NonSubscription(t *testing.T) {
	svc, _ := newTestSubscriptionService()

	data, _ := json.Marshal(map[string]interface{}{
		"voidedPurchaseNotification": map[string]interface{}{
			"orderId": "GPA.1234",
		},
	})

	msg := GoogleRTDNMessage{}
	msg.Message.Data = base64.StdEncoding.EncodeToString(data)

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected nil error for non-subscription notification, got: %v", err)
	}
}

// --- Edge case: invalid Apple JWS format ---

func TestHandleAppleNotification_InvalidJWS(t *testing.T) {
	svc, _ := newTestSubscriptionService()

	err := svc.HandleAppleNotification(context.Background(), "not-a-valid-jws")
	if err == nil {
		t.Fatal("expected error for invalid JWS, got nil")
	}
	if !strings.Contains(err.Error(), "decode notification") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

// --- Edge case: invalid Google base64 data ---

func TestHandleGoogleNotification_InvalidBase64(t *testing.T) {
	svc, _ := newTestSubscriptionService()

	msg := GoogleRTDNMessage{}
	msg.Message.Data = "!!! not base64 !!!"

	err := svc.HandleGoogleNotification(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
	if !strings.Contains(err.Error(), "decode RTDN data") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

// --- Edge case: already expired subscription receiving EXPIRED again ---

func TestHandleAppleNotification_AlreadyExpired(t *testing.T) {
	svc, shopRepo := newTestSubscriptionService()
	sub := createTestSubscription(shopRepo, "p1", "apple-token-already-exp")

	// Pre-expire the subscription
	sub.Status = model.SubscriptionStatusExpired
	_ = shopRepo.UpdateSubscription(context.Background(), sub)
	_ = shopRepo.UpdatePlayerPremium(context.Background(), "p1", false, nil)

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

	err := svc.HandleAppleNotification(context.Background(), notifPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedSub, _ := shopRepo.FindSubscriptionByToken(context.Background(), sub.PurchaseToken)
	if updatedSub.Status != model.SubscriptionStatusExpired {
		t.Errorf("expected status to remain expired, got %s", updatedSub.Status)
	}

	pd := shopRepo.GetPlayerDataForTest("p1")
	if pd == nil || pd.IsPremium {
		t.Error("expected player to remain non-premium")
	}
}
