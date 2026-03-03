package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kenyamaneko/overload-party-common/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

type SubscriptionService struct {
	shopRepo repository.ShopRepository
}

func NewSubscriptionService(shopRepo repository.ShopRepository) *SubscriptionService {
	return &SubscriptionService{shopRepo: shopRepo}
}

// AppleNotificationPayload is the outer structure of an Apple webhook.
type AppleNotificationPayload struct {
	SignedPayload string `json:"signedPayload"`
}

// appleNotification is the decoded JWS payload from Apple.
type appleNotification struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	Data             struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	} `json:"data"`
}

// appleNotificationTxn is the decoded transaction info from the notification.
type appleNotificationTxn struct {
	OriginalTransactionID string `json:"originalTransactionId"`
	ExpiresDate           int64  `json:"expiresDate"`
}

// HandleAppleNotification processes an Apple App Store Server Notification V2.
func (s *SubscriptionService) HandleAppleNotification(ctx context.Context, signedPayload string) error {
	// Decode the JWS payload (without full signature verification for now)
	notif, err := decodeAppleNotification(signedPayload)
	if err != nil {
		return fmt.Errorf("decode notification: %w", err)
	}

	// Decode the transaction info from the notification
	txnInfo, err := decodeAppleNotificationTxn(notif.Data.SignedTransactionInfo)
	if err != nil {
		return fmt.Errorf("decode transaction info: %w", err)
	}

	// Find the subscription by the original transaction ID (stored as purchase_token)
	sub, err := s.shopRepo.FindSubscriptionByToken(ctx, txnInfo.OriginalTransactionID)
	if err != nil {
		return fmt.Errorf("find subscription: %w", err)
	}
	if sub == nil {
		return fmt.Errorf("subscription not found for token: %s", txnInfo.OriginalTransactionID)
	}

	switch notif.NotificationType {
	case "DID_RENEW":
		expiresAt := time.UnixMilli(txnInfo.ExpiresDate)
		sub.CurrentPeriodEnd = expiresAt
		sub.Status = model.SubscriptionStatusActive
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.shopRepo.UpdatePlayerPremium(ctx, sub.PlayerID, true, &expiresAt); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case "EXPIRED", "GRACE_PERIOD_EXPIRED":
		sub.Status = model.SubscriptionStatusExpired
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.shopRepo.UpdatePlayerPremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case "REVOKE":
		sub.Status = model.SubscriptionStatusRevoked
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.shopRepo.UpdatePlayerPremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case "DID_CHANGE_RENEWAL_STATUS":
		if notif.Subtype == "AUTO_RENEW_DISABLED" {
			sub.Status = model.SubscriptionStatusCancelled
			sub.UpdatedAt = time.Now()
			if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
				return fmt.Errorf("update subscription: %w", err)
			}
			// Premium remains active until current_period_end
		}
	}

	return nil
}

// GoogleRTDNMessage represents a Google Play Real-time Developer Notification push message.
type GoogleRTDNMessage struct {
	Message struct {
		Data string `json:"data"`
	} `json:"message"`
}

// googleRTDNData is the decoded data from a Google RTDN message.
type googleRTDNData struct {
	SubscriptionNotification *struct {
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		SubscriptionID   string `json:"subscriptionId"`
	} `json:"subscriptionNotification"`
}

// Google RTDN notification types
const (
	googleSubRecovered = 2
	googleSubCanceled  = 3
	googleSubRenewed   = 4
	googleSubExpired   = 12
	googleSubRevoked   = 13
)

// HandleGoogleNotification processes a Google Play RTDN message.
func (s *SubscriptionService) HandleGoogleNotification(ctx context.Context, msg GoogleRTDNMessage) error {
	data, err := base64.StdEncoding.DecodeString(msg.Message.Data)
	if err != nil {
		return fmt.Errorf("decode RTDN data: %w", err)
	}

	var rtdn googleRTDNData
	if err := json.Unmarshal(data, &rtdn); err != nil {
		return fmt.Errorf("unmarshal RTDN data: %w", err)
	}

	if rtdn.SubscriptionNotification == nil {
		return nil // Not a subscription notification
	}

	notif := rtdn.SubscriptionNotification
	sub, err := s.shopRepo.FindSubscriptionByToken(ctx, notif.PurchaseToken)
	if err != nil {
		return fmt.Errorf("find subscription: %w", err)
	}
	if sub == nil {
		return fmt.Errorf("subscription not found for token: %s", notif.PurchaseToken)
	}

	switch notif.NotificationType {
	case googleSubRenewed, googleSubRecovered:
		sub.Status = model.SubscriptionStatusActive
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		// For renewal, we should verify with Google API to get the new expiry.
		// For now, extend by 30 days as a reasonable default.
		newExpiry := time.Now().Add(30 * 24 * time.Hour)
		if err := s.shopRepo.UpdatePlayerPremium(ctx, sub.PlayerID, true, &newExpiry); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case googleSubExpired:
		sub.Status = model.SubscriptionStatusExpired
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.shopRepo.UpdatePlayerPremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case googleSubRevoked:
		sub.Status = model.SubscriptionStatusRevoked
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.shopRepo.UpdatePlayerPremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case googleSubCanceled:
		sub.Status = model.SubscriptionStatusCancelled
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		// Premium remains active until current_period_end
	}

	return nil
}

func decodeAppleNotification(jws string) (*appleNotification, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}
	var notif appleNotification
	if err := json.Unmarshal(payload, &notif); err != nil {
		return nil, fmt.Errorf("unmarshal notification: %w", err)
	}
	return &notif, nil
}

func decodeAppleNotificationTxn(jws string) (*appleNotificationTxn, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}
	var txn appleNotificationTxn
	if err := json.Unmarshal(payload, &txn); err != nil {
		return nil, fmt.Errorf("unmarshal transaction: %w", err)
	}
	return &txn, nil
}
