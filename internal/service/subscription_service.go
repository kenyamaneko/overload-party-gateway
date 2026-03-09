package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// Apple App Store Server Notification V2 types.
const (
	appleNotifDIDRenew              = "DID_RENEW"
	appleNotifExpired               = "EXPIRED"
	appleNotifGracePeriodExpired    = "GRACE_PERIOD_EXPIRED"
	appleNotifRevoke                = "REVOKE"
	appleNotifDIDChangeRenewStatus  = "DID_CHANGE_RENEWAL_STATUS"
	appleSubtypeAutoRenewDisabled   = "AUTO_RENEW_DISABLED"
)

// Google サブスク更新時のデフォルト延長期間。
// TODO: Google Play Developer API で実際の有効期限を取得して置き換える。
const googleSubRenewalExtension = 30 * 24 * time.Hour

type SubscriptionService struct {
	shopRepo   repository.ShopRepository
	playerRepo repository.PlayerRepo
}

func NewSubscriptionService(shopRepo repository.ShopRepository, playerRepo repository.PlayerRepo) *SubscriptionService {
	return &SubscriptionService{shopRepo: shopRepo, playerRepo: playerRepo}
}

type AppleNotificationPayload struct {
	SignedPayload string `json:"signedPayload"`
}

type appleNotification struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	Data             struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	} `json:"data"`
}

type appleNotificationTxn struct {
	OriginalTransactionID string `json:"originalTransactionId"`
	ExpiresDate           int64  `json:"expiresDate"`
}

func (s *SubscriptionService) HandleAppleNotification(ctx context.Context, signedPayload string) error {
	notif, err := decodeAppleNotification(signedPayload)
	if err != nil {
		return fmt.Errorf("decode notification: %w", err)
	}

	txnInfo, err := decodeAppleNotificationTxn(notif.Data.SignedTransactionInfo)
	if err != nil {
		return fmt.Errorf("decode transaction info: %w", err)
	}

	sub, err := s.shopRepo.FindSubscriptionByToken(ctx, txnInfo.OriginalTransactionID)
	if err != nil {
		return fmt.Errorf("find subscription: %w", err)
	}
	if sub == nil {
		return fmt.Errorf("subscription not found for token: %s", txnInfo.OriginalTransactionID)
	}

	switch notif.NotificationType {
	case appleNotifDIDRenew:
		expiresAt := time.UnixMilli(txnInfo.ExpiresDate)
		sub.CurrentPeriodEnd = expiresAt
		sub.Status = model.SubscriptionStatusActive
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, true, &expiresAt); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case appleNotifExpired, appleNotifGracePeriodExpired:
		sub.Status = model.SubscriptionStatusExpired
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case appleNotifRevoke:
		sub.Status = model.SubscriptionStatusRevoked
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case appleNotifDIDChangeRenewStatus:
		if notif.Subtype == appleSubtypeAutoRenewDisabled {
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

type GoogleRTDNMessage struct {
	Message struct {
		Data string `json:"data"`
	} `json:"message"`
}

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
		newExpiry := time.Now().Add(googleSubRenewalExtension)
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, true, &newExpiry); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case googleSubExpired:
		sub.Status = model.SubscriptionStatusExpired
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, false, nil); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}

	case googleSubRevoked:
		sub.Status = model.SubscriptionStatusRevoked
		sub.UpdatedAt = time.Now()
		if err := s.shopRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, false, nil); err != nil {
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
