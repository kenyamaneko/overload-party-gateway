package repository

import (
	"context"
	"fmt"
	"sort"

	"github.com/kenyamaneko/overload-party-common/model"
)

// MockCardRepository is an in-memory implementation of CardRepo backed by a card map.
type MockCardRepository struct {
	cards map[int64]*model.CardDefinition
}

var _ CardRepo = (*MockCardRepository)(nil)

func NewMockCardRepository(cards map[int64]*model.CardDefinition) *MockCardRepository {
	return &MockCardRepository{cards: cards}
}

func (r *MockCardRepository) FindAll(ctx context.Context) ([]*model.CardDefinition, error) {
	var result []*model.CardDefinition
	for _, c := range r.cards {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CardNo < result[j].CardNo
	})
	return result, nil
}

func (r *MockCardRepository) FindByCardNo(ctx context.Context, cardNo int64) (*model.CardDefinition, error) {
	c, ok := r.cards[cardNo]
	if !ok {
		return nil, fmt.Errorf("card %d not found", cardNo)
	}
	return c, nil
}
