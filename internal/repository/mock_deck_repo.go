package repository

import (
	"context"
	"fmt"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

func deckKey(playerID string, deckID int64) string {
	return fmt.Sprintf("%s|%d", playerID, deckID)
}

// MockDeckRepository is an in-memory implementation of DeckRepo for local mode and testing.
type MockDeckRepository struct {
	mu          sync.Mutex
	nextDeckID  int64
	decks       map[string]*model.Deck         // "playerID|deckID" → Deck
	deckCards   map[string][]model.DeckCard    // "playerID|deckID" → DeckCards
	playerCards map[string][]*model.PlayerCard // playerID → PlayerCards
}

var _ DeckRepo = (*MockDeckRepository)(nil)

func NewMockDeckRepository() *MockDeckRepository {
	return &MockDeckRepository{
		decks:       make(map[string]*model.Deck),
		deckCards:   make(map[string][]model.DeckCard),
		playerCards: make(map[string][]*model.PlayerCard),
	}
}

func (r *MockDeckRepository) Create(ctx context.Context, deck *model.Deck, cards []model.DeckCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextDeckID++
	deck.DeckID = r.nextDeckID

	// Set DeckID on cards
	for i := range cards {
		cards[i].DeckID = deck.DeckID
	}

	key := deckKey(deck.PlayerID, deck.DeckID)
	r.decks[key] = deck
	r.deckCards[key] = cards
	return nil
}

func (r *MockDeckRepository) FindByPlayerID(ctx context.Context, playerID string) ([]*model.Deck, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []*model.Deck
	for _, d := range r.decks {
		if d.PlayerID == playerID {
			result = append(result, d)
		}
	}
	return result, nil
}

func (r *MockDeckRepository) FindByID(ctx context.Context, playerID string, deckID int64) (*model.Deck, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.decks[deckKey(playerID, deckID)]
	if !ok {
		return nil, fmt.Errorf("deck %d not found for player %s", deckID, playerID)
	}
	return d, nil
}

func (r *MockDeckRepository) GetDeckCards(ctx context.Context, playerID string, deckID int64) ([]model.DeckCard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cards, ok := r.deckCards[deckKey(playerID, deckID)]
	if !ok {
		return nil, nil
	}
	return cards, nil
}

func (r *MockDeckRepository) GetDeckCardNos(ctx context.Context, playerID string, deckID int64) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cards, ok := r.deckCards[deckKey(playerID, deckID)]
	if !ok {
		return nil, fmt.Errorf("deck %d not found for player %s", deckID, playerID)
	}

	var cardNos []int64
	for _, dc := range cards {
		for i := 0; i < dc.Count; i++ {
			cardNos = append(cardNos, dc.CardNo)
		}
	}
	return cardNos, nil
}

func (r *MockDeckRepository) GetPlayerCards(ctx context.Context, playerID string) ([]*model.PlayerCard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.playerCards[playerID], nil
}

func (r *MockDeckRepository) Update(ctx context.Context, deck *model.Deck, cards []model.DeckCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := deckKey(deck.PlayerID, deck.DeckID)
	r.decks[key] = deck
	r.deckCards[key] = cards
	return nil
}

func (r *MockDeckRepository) Delete(ctx context.Context, playerID string, deckID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := deckKey(playerID, deckID)
	delete(r.decks, key)
	delete(r.deckCards, key)
	return nil
}

// SeedPlayerCards adds player cards to the mock repository (for local mode initialization).
// Merges counts for existing (card_no, illustration_variant) pairs.
func (r *MockDeckRepository) SeedPlayerCards(playerID string, cards []*model.PlayerCard) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.playerCards[playerID]
	for _, newCard := range cards {
		found := false
		for _, ex := range existing {
			if ex.CardNo == newCard.CardNo && ex.IllustrationVariant == newCard.IllustrationVariant {
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
