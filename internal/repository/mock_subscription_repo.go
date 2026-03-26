package repository

import (
	"context"
	"fmt"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// MockSubscriptionRepository is an in-memory implementation of SubscriptionRepo for testing.
type MockSubscriptionRepository struct {
	mu                 sync.Mutex
	nextSubscriptionID int64
	subscriptions      map[string][]*model.Subscription // keyed by playerID
}

// Compile-time interface check.
var _ port.SubscriptionRepo = (*MockSubscriptionRepository)(nil)

func NewMockSubscriptionRepository() *MockSubscriptionRepository {
	return &MockSubscriptionRepository{
		subscriptions: make(map[string][]*model.Subscription),
	}
}

func (r *MockSubscriptionRepository) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSubscriptionID++
	sub.SubscriptionID = r.nextSubscriptionID
	r.subscriptions[sub.PlayerID] = append(r.subscriptions[sub.PlayerID], sub)
	return nil
}

func (r *MockSubscriptionRepository) GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.subscriptions[playerID] {
		if s.Status == model.SubscriptionStatusActive {
			return s, nil
		}
	}
	return nil, nil
}

func (r *MockSubscriptionRepository) FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, subs := range r.subscriptions {
		for _, s := range subs {
			if s.PurchaseToken == purchaseToken {
				return s, nil
			}
		}
	}
	return nil, nil
}

func (r *MockSubscriptionRepository) UpdateSubscription(ctx context.Context, sub *model.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, subs := range r.subscriptions {
		for i, s := range subs {
			if s.SubscriptionID == sub.SubscriptionID {
				subs[i] = sub
				return nil
			}
		}
	}
	return fmt.Errorf("subscription %d not found", sub.SubscriptionID)
}
