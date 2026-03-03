package platform

import (
	"context"
	"time"
)

// VerifyResult holds the result of a one-time purchase receipt verification.
type VerifyResult struct {
	IsValid       bool
	TransactionID string
	ProductID     string
	PurchaseTime  time.Time
}

// SubscriptionInfo holds subscription-specific verification results.
type SubscriptionInfo struct {
	IsValid        bool
	ProductID      string
	TransactionID  string
	ExpiresAt      time.Time
	IsAutoRenewing bool
}

// ReceiptVerifier abstracts receipt/purchase verification across platforms.
type ReceiptVerifier interface {
	// VerifyPurchase validates a one-time purchase receipt.
	VerifyPurchase(ctx context.Context, purchaseToken string) (*VerifyResult, error)

	// VerifySubscription validates a subscription receipt.
	VerifySubscription(ctx context.Context, purchaseToken string) (*SubscriptionInfo, error)
}
