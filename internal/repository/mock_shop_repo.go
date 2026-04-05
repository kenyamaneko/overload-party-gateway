package repository

import (
	"context"
	"fmt"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// MockShopRepository is an in-memory implementation of ShopRepository for testing.
type MockShopRepository struct {
	mu             sync.Mutex
	nextPurchaseID int64
	products       map[string]*model.Product
	purchases      map[string][]*model.OneTimePurchase // keyed by playerID
	playerCards    map[string][]*model.PlayerCard      // keyed by playerID
	playerItems    map[string][]*model.PlayerItem      // keyed by playerID

	// onCardsInserted is called after cards are inserted, allowing cross-repo sync.
	onCardsInserted func(playerID string, cards []*model.PlayerCard)
}

// Compile-time interface check.
var _ port.ShopRepository = (*MockShopRepository)(nil)

func NewMockShopRepository() *MockShopRepository {
	return &MockShopRepository{
		products:    make(map[string]*model.Product),
		purchases:   make(map[string][]*model.OneTimePurchase),
		playerCards: make(map[string][]*model.PlayerCard),
		playerItems: make(map[string][]*model.PlayerItem),
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
		return nil, fmt.Errorf("product %s: %w", productID, port.ErrNotFound)
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

// mergePlayerCards merges card counts for existing (card_id, art_no) pairs.
// Must be called with r.mu held.
func (r *MockShopRepository) mergePlayerCards(playerID string, cards []*model.PlayerCard) {
	existing := r.playerCards[playerID]
	for _, newCard := range cards {
		found := false
		for _, ex := range existing {
			if ex.CardID == newCard.CardID && ex.ArtNo == newCard.ArtNo {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items {
		r.playerItems[item.PlayerID] = append(r.playerItems[item.PlayerID], item)
	}
	return nil
}

func (r *MockShopRepository) HasPlayerItem(ctx context.Context, playerID, itemType string, itemNo int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.playerItems[playerID] {
		if item.ItemType == itemType && item.ItemNo == itemNo {
			return true, nil
		}
	}
	return false, nil
}

