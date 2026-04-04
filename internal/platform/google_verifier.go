package platform

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/option"
)

// GoogleReceiptVerifier implements ReceiptVerifier using Google Play Developer API.
type GoogleReceiptVerifier struct {
	service     *androidpublisher.Service
	packageName string
}

// NewGoogleReceiptVerifier creates a new Google Play receipt verifier.
// It uses Application Default Credentials (ADC) for authentication.
func NewGoogleReceiptVerifier(ctx context.Context, packageName string, opts ...option.ClientOption) (*GoogleReceiptVerifier, error) {
	svc, err := androidpublisher.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create androidpublisher service: %w", err)
	}

	return &GoogleReceiptVerifier{
		service:     svc,
		packageName: packageName,
	}, nil
}

// Compile-time interface check.
var _ ReceiptVerifier = (*GoogleReceiptVerifier)(nil)

func (v *GoogleReceiptVerifier) VerifyPurchase(ctx context.Context, purchaseToken string) (*VerifyResult, error) {
	// For Google Play, purchaseToken is "{productId}:{token}" format
	productID, token, err := splitGoogleToken(purchaseToken)
	if err != nil {
		return &VerifyResult{IsValid: false}, err
	}

	result, err := v.service.Purchases.Products.Get(v.packageName, productID, token).Context(ctx).Do()
	if err != nil {
		return &VerifyResult{IsValid: false}, fmt.Errorf("Google Play API: %w", err)
	}

	// PurchaseState: 0=Purchased, 1=Canceled, 2=Pending
	if result.PurchaseState != 0 {
		return &VerifyResult{IsValid: false}, nil
	}

	purchaseTime := time.UnixMilli(result.PurchaseTimeMillis)

	return &VerifyResult{
		IsValid:       true,
		TransactionID: result.OrderId,
		ProductID:     productID,
		PurchaseTime:  purchaseTime,
	}, nil
}

func (v *GoogleReceiptVerifier) VerifySubscription(ctx context.Context, purchaseToken string) (*SubscriptionInfo, error) {
	// For subscriptions, purchaseToken is "{subscriptionId}:{token}" format
	subscriptionID, token, err := splitGoogleToken(purchaseToken)
	if err != nil {
		return &SubscriptionInfo{IsValid: false}, err
	}

	result, err := v.service.Purchases.Subscriptions.Get(v.packageName, subscriptionID, token).Context(ctx).Do()
	if err != nil {
		return &SubscriptionInfo{IsValid: false}, fmt.Errorf("Google Play API: %w", err)
	}

	expiresAt := time.UnixMilli(result.ExpiryTimeMillis)

	return &SubscriptionInfo{
		IsValid:        true,
		ProductID:      subscriptionID,
		TransactionID:  result.OrderId,
		ExpiresAt:      expiresAt,
		IsAutoRenewing: result.AutoRenewing,
	}, nil
}

func splitGoogleToken(composite string) (string, string, error) {
	productID, token, ok := strings.Cut(composite, ":")
	if !ok {
		return "", "", fmt.Errorf("invalid Google purchase token format: expected 'productId:token'")
	}
	return productID, token, nil
}

// GooglePlaySubVerifier fetches actual subscription expiry from Google Play Developer API.
type GooglePlaySubVerifier struct {
	service     *androidpublisher.Service
	packageName string
}

// NewGooglePlaySubVerifier creates a verifier that queries Google Play for subscription expiry.
// Uses Application Default Credentials (ADC) for authentication.
func NewGooglePlaySubVerifier(ctx context.Context, packageName string, opts ...option.ClientOption) (*GooglePlaySubVerifier, error) {
	svc, err := androidpublisher.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create androidpublisher service: %w", err)
	}
	return &GooglePlaySubVerifier{
		service:     svc,
		packageName: packageName,
	}, nil
}

func (v *GooglePlaySubVerifier) GetSubscriptionExpiry(ctx context.Context, purchaseToken string) (time.Time, error) {
	subscriptionID, token, err := splitGoogleToken(purchaseToken)
	if err != nil {
		return time.Time{}, err
	}

	result, err := v.service.Purchases.Subscriptions.Get(v.packageName, subscriptionID, token).Context(ctx).Do()
	if err != nil {
		return time.Time{}, fmt.Errorf("Google Play API get subscription: %w", err)
	}

	return time.UnixMilli(result.ExpiryTimeMillis), nil
}
