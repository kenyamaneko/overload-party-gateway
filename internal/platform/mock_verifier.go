package platform

import "context"

// MockReceiptVerifier implements ReceiptVerifier for testing.
type MockReceiptVerifier struct {
	VerifyPurchaseFn     func(ctx context.Context, purchaseToken string) (*VerifyResult, error)
	VerifySubscriptionFn func(ctx context.Context, purchaseToken string) (*SubscriptionInfo, error)
}

// Compile-time interface check.
var _ ReceiptVerifier = (*MockReceiptVerifier)(nil)

func (m *MockReceiptVerifier) VerifyPurchase(ctx context.Context, purchaseToken string) (*VerifyResult, error) {
	if m.VerifyPurchaseFn != nil {
		return m.VerifyPurchaseFn(ctx, purchaseToken)
	}
	return &VerifyResult{IsValid: true, TransactionID: "mock-txn-id", ProductID: "mock-product"}, nil
}

func (m *MockReceiptVerifier) VerifySubscription(ctx context.Context, purchaseToken string) (*SubscriptionInfo, error) {
	if m.VerifySubscriptionFn != nil {
		return m.VerifySubscriptionFn(ctx, purchaseToken)
	}
	return &SubscriptionInfo{IsValid: true, ProductID: "mock-sub", TransactionID: "mock-sub-txn"}, nil
}
