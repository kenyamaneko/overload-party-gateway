package repository

import (
	"context"
	"fmt"
	"sort"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// MockCardRepository is an in-memory implementation of CardRepo backed by a card map.
type MockCardRepository struct {
	cards map[string]*model.CardDefinition
}

var _ CardRepo = (*MockCardRepository)(nil)

func NewMockCardRepository(cards map[string]*model.CardDefinition) *MockCardRepository {
	return &MockCardRepository{cards: cards}
}

func (r *MockCardRepository) FindAll(ctx context.Context) ([]*model.CardDefinition, error) {
	var result []*model.CardDefinition
	for _, c := range r.cards {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CardID < result[j].CardID
	})
	return result, nil
}

func (r *MockCardRepository) FindByCardID(ctx context.Context, cardID string) (*model.CardDefinition, error) {
	c, ok := r.cards[cardID]
	if !ok {
		return nil, fmt.Errorf("card %s not found", cardID)
	}
	return c, nil
}
