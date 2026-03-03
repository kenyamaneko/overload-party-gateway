package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// CardCache provides in-memory access to card definitions.
// Loaded once at startup, refreshed periodically.
type CardCache struct {
	mu    sync.RWMutex
	cards map[int64]*model.CardDefinition
}

func NewCardCache() *CardCache {
	return &CardCache{cards: make(map[int64]*model.CardDefinition)}
}

// Get returns a card definition by card_no. Returns nil if not found.
func (c *CardCache) Get(cardNo int64) *model.CardDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cards[cardNo]
}

// MustGet returns a card definition by card_no, panicking if not found.
// Use only when the card_no is known to be valid (e.g., from a deck snapshot).
func (c *CardCache) MustGet(cardNo int64) *model.CardDefinition {
	card := c.Get(cardNo)
	if card == nil {
		panic(fmt.Sprintf("card_no %d not found in cache", cardNo))
	}
	return card
}

// Load fetches all active cards via the CardRepo interface.
func (c *CardCache) Load(ctx context.Context, repo repository.CardRepo) error {
	cards, err := repo.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("load card cache: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cards = make(map[int64]*model.CardDefinition, len(cards))
	for _, card := range cards {
		c.cards[card.CardNo] = card
	}

	log.Printf("card cache loaded: %d cards", len(c.cards))
	return nil
}

// Refresh reloads all cards via the CardRepo interface.
func (c *CardCache) Refresh(ctx context.Context, repo repository.CardRepo) error {
	return c.Load(ctx, repo)
}

// All returns a snapshot of all cached cards.
func (c *CardCache) All() map[int64]*model.CardDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot := make(map[int64]*model.CardDefinition, len(c.cards))
	for k, v := range c.cards {
		snapshot[k] = v
	}
	return snapshot
}

// Count returns the number of cached cards.
func (c *CardCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cards)
}

// LoadFromJSON loads card definitions from a JSON file.
func (c *CardCache) LoadFromJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read card file: %w", err)
	}

	var cards []*model.CardDefinition
	if err := json.Unmarshal(data, &cards); err != nil {
		return fmt.Errorf("parse card file: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cards = make(map[int64]*model.CardDefinition, len(cards))
	for _, card := range cards {
		c.cards[card.CardNo] = card
	}

	log.Printf("card cache loaded from JSON: %d cards", len(c.cards))
	return nil
}

// InjectForTest inserts a card definition directly into the cache.
// For use in unit tests only.
func (c *CardCache) InjectForTest(cardNo int64, card *model.CardDefinition) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cards[cardNo] = card
}
