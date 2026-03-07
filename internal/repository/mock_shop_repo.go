package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// MockShopRepository is an in-memory implementation of ShopRepository for testing.
type MockShopRepository struct {
	mu                 sync.Mutex
	nextPurchaseID     int64
	nextSubscriptionID int64
	products           map[string]*model.Product
	purchases          map[string][]*model.OneTimePurchase // keyed by playerID
	playerCards        map[string][]*model.PlayerCard      // keyed by playerID
	playerItems        map[string][]*model.PlayerItem      // keyed by playerID
	subscriptions      map[string][]*model.Subscription    // keyed by playerID

	// onCardsInserted is called after cards are inserted, allowing cross-repo sync.
	onCardsInserted func(playerID string, cards []*model.PlayerCard)
}

// Compile-time interface check.
var _ ShopRepository = (*MockShopRepository)(nil)

func NewMockShopRepository() *MockShopRepository {
	return &MockShopRepository{
		products:      make(map[string]*model.Product),
		purchases:     make(map[string][]*model.OneTimePurchase),
		playerCards:   make(map[string][]*model.PlayerCard),
		playerItems:   make(map[string][]*model.PlayerItem),
		subscriptions: make(map[string][]*model.Subscription),
	}
}

// SetOnCardsInserted sets a callback invoked whenever player cards are inserted.
// Used to bridge MockShopRepository and MockDeckRepository in local mode.
func (r *MockShopRepository) SetOnCardsInserted(fn func(playerID string, cards []*model.PlayerCard)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onCardsInserted = fn
}

// --- Test helpers ---

// AddProduct pre-populates a product for tests.
func (r *MockShopRepository) AddProduct(product *model.Product) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.products[product.ProductID] = product
}

// GetPlayerCards returns cards owned by a player (test helper).
func (r *MockShopRepository) GetPlayerCardsForTest(playerID string) []*model.PlayerCard {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.playerCards[playerID]
}

// --- ShopRepository implementation ---

func (r *MockShopRepository) GetActiveProducts(ctx context.Context) ([]*model.Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*model.Product
	for _, p := range r.products {
		if p.IsActive {
			result = append(result, p)
		}
	}
	return result, nil
}

func (r *MockShopRepository) GetProductByID(ctx context.Context, productID string) (*model.Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.products[productID]
	if !ok {
		return nil, fmt.Errorf("product %s not found", productID)
	}
	return p, nil
}

func (r *MockShopRepository) FindPurchaseByToken(ctx context.Context, playerID, purchaseToken string) (*model.OneTimePurchase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.purchases[playerID] {
		if p.PurchaseToken == purchaseToken {
			return p, nil
		}
	}
	return nil, nil
}

func (r *MockShopRepository) CreatePurchaseWithCards(ctx context.Context, purchase *model.OneTimePurchase, cards []*model.PlayerCard) error {
	r.mu.Lock()
	// Idempotency check
	for _, p := range r.purchases[purchase.PlayerID] {
		if p.PurchaseToken == purchase.PurchaseToken {
			r.mu.Unlock()
			return nil
		}
	}
	r.nextPurchaseID++
	purchase.PurchaseID = r.nextPurchaseID
	r.purchases[purchase.PlayerID] = append(r.purchases[purchase.PlayerID], purchase)
	r.mergePlayerCards(purchase.PlayerID, cards)
	cb := r.onCardsInserted
	r.mu.Unlock()

	if cb != nil && len(cards) > 0 {
		cb(purchase.PlayerID, cards)
	}
	return nil
}

func (r *MockShopRepository) CreatePurchaseWithItem(ctx context.Context, purchase *model.OneTimePurchase, item *model.PlayerItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.purchases[purchase.PlayerID] {
		if p.PurchaseToken == purchase.PurchaseToken {
			return nil
		}
	}
	r.nextPurchaseID++
	purchase.PurchaseID = r.nextPurchaseID
	r.purchases[purchase.PlayerID] = append(r.purchases[purchase.PlayerID], purchase)
	r.playerItems[purchase.PlayerID] = append(r.playerItems[purchase.PlayerID], item)
	return nil
}

func (r *MockShopRepository) InsertPlayerCards(ctx context.Context, cards []*model.PlayerCard) error {
	r.mu.Lock()
	if len(cards) == 0 {
		r.mu.Unlock()
		return nil
	}
	playerID := cards[0].PlayerID
	r.mergePlayerCards(playerID, cards)
	cb := r.onCardsInserted
	r.mu.Unlock()

	if cb != nil {
		cb(playerID, cards)
	}
	return nil
}

// mergePlayerCards merges card counts for existing (card_no, art_no) pairs.
// Must be called with r.mu held.
func (r *MockShopRepository) mergePlayerCards(playerID string, cards []*model.PlayerCard) {
	existing := r.playerCards[playerID]
	for _, newCard := range cards {
		found := false
		for _, ex := range existing {
			if ex.CardNo == newCard.CardNo && ex.ArtNo == newCard.ArtNo {
				ex.Count += newCard.Count
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, newCard)
		}
	}
	r.playerCards[playerID] = existing
}

func (r *MockShopRepository) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	return nil
}

func (r *MockShopRepository) GetPlayerOwnedFactions(ctx context.Context, playerID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	factionSet := make(map[string]bool)
	for _, purchase := range r.purchases[playerID] {
		product, ok := r.products[purchase.ProductID]
		if ok && product.Type == model.ProductTypeFactionSet {
			var content model.FactionSetContent
			if err := parseJSON(product.Content, &content); err == nil {
				factionSet[content.Faction] = true
			}
		}
	}
	var factions []string
	for f := range factionSet {
		factions = append(factions, f)
	}
	return factions, nil
}

func (r *MockShopRepository) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSubscriptionID++
	sub.SubscriptionID = r.nextSubscriptionID
	r.subscriptions[sub.PlayerID] = append(r.subscriptions[sub.PlayerID], sub)
	return nil
}

func (r *MockShopRepository) GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.subscriptions[playerID] {
		if s.Status == model.SubscriptionStatusActive {
			return s, nil
		}
	}
	return nil, nil
}

func (r *MockShopRepository) FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error) {
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

func (r *MockShopRepository) UpdateSubscription(ctx context.Context, sub *model.Subscription) error {
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


func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
