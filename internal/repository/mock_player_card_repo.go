package repository

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// MockPlayerCardRepository is an in-memory implementation of PlayerCardRepo for local mode and testing.
type MockPlayerCardRepository struct {
	mu          sync.Mutex
	playerCards map[string][]*model.PlayerCard // playerID → PlayerCards
}

var _ PlayerCardRepo = (*MockPlayerCardRepository)(nil)

func NewMockPlayerCardRepository() *MockPlayerCardRepository {
	return &MockPlayerCardRepository{
		playerCards: make(map[string][]*model.PlayerCard),
	}
}

func (r *MockPlayerCardRepository) GetPlayerCards(ctx context.Context, playerID string) ([]*model.PlayerCard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.playerCards[playerID], nil
}

// SeedPlayerCards adds player cards to the mock repository (for local mode initialization).
// Merges counts for existing (card_id, art_no) pairs.
func (r *MockPlayerCardRepository) SeedPlayerCards(playerID string, cards []*model.PlayerCard) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
