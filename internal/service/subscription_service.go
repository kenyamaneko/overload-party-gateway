package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
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
	subRepo    port.SubscriptionRepo
	playerRepo port.PlayerRepo
	txRunner   port.TxRunner
}

func NewSubscriptionService(subRepo port.SubscriptionRepo, playerRepo port.PlayerRepo, txRunner port.TxRunner) *SubscriptionService {
	return &SubscriptionService{subRepo: subRepo, playerRepo: playerRepo, txRunner: txRunner}
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

// updateSubAndPremium atomically updates both the subscription record and the player's premium status.
func (s *SubscriptionService) updateSubAndPremium(ctx context.Context, sub *model.Subscription, isPremium bool, expiresAt *time.Time) error {
	return s.txRunner.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.subRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if err := s.playerRepo.UpdatePremium(ctx, sub.PlayerID, isPremium, expiresAt); err != nil {
			return fmt.Errorf("update player premium: %w", err)
		}
		return nil
	})
}

func (s *SubscriptionService) HandleAppleNotification(ctx context.Context, signedPayload string) error {
	notif, err := decodeJWSPayload[appleNotification](signedPayload)
	if err != nil {
		return fmt.Errorf("decode notification: %w", err)
	}

	txnInfo, err := decodeJWSPayload[appleNotificationTxn](notif.Data.SignedTransactionInfo)
	if err != nil {
		return fmt.Errorf("decode transaction info: %w", err)
	}

	sub, err := s.subRepo.FindSubscriptionByToken(ctx, txnInfo.OriginalTransactionID)
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
		if err := s.updateSubAndPremium(ctx, sub, true, &expiresAt); err != nil {
			return err
		}

	case appleNotifExpired, appleNotifGracePeriodExpired:
		sub.Status = model.SubscriptionStatusExpired
		sub.UpdatedAt = time.Now()
		if err := s.updateSubAndPremium(ctx, sub, false, nil); err != nil {
			return err
		}

	case appleNotifRevoke:
		sub.Status = model.SubscriptionStatusRevoked
		sub.UpdatedAt = time.Now()
		if err := s.updateSubAndPremium(ctx, sub, false, nil); err != nil {
			return err
		}

	case appleNotifDIDChangeRenewStatus:
		if notif.Subtype == appleSubtypeAutoRenewDisabled {
			sub.Status = model.SubscriptionStatusCancelled
			sub.UpdatedAt = time.Now()
			if err := s.subRepo.UpdateSubscription(ctx, sub); err != nil {
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
	sub, err := s.subRepo.FindSubscriptionByToken(ctx, notif.PurchaseToken)
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
		// For renewal, we should verify with Google API to get the new expiry.
		// For now, extend by 30 days as a reasonable default.
		newExpiry := time.Now().Add(googleSubRenewalExtension)
		if err := s.updateSubAndPremium(ctx, sub, true, &newExpiry); err != nil {
			return err
		}

	case googleSubExpired:
		sub.Status = model.SubscriptionStatusExpired
		sub.UpdatedAt = time.Now()
		if err := s.updateSubAndPremium(ctx, sub, false, nil); err != nil {
			return err
		}

	case googleSubRevoked:
		sub.Status = model.SubscriptionStatusRevoked
		sub.UpdatedAt = time.Now()
		if err := s.updateSubAndPremium(ctx, sub, false, nil); err != nil {
			return err
		}

	case googleSubCanceled:
		sub.Status = model.SubscriptionStatusCancelled
		sub.UpdatedAt = time.Now()
		if err := s.subRepo.UpdateSubscription(ctx, sub); err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		// Premium remains active until current_period_end
	}

	return nil
}

// decodeJWSPayload extracts and unmarshals the payload section of a JWS token.
func decodeJWSPayload[T any](jws string) (*T, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}
	var v T
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, fmt.Errorf("unmarshal JWS payload: %w", err)
	}
	return &v, nil
}
