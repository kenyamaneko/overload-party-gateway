package service

import (
	"context"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAllCards_WithOwnership(t *testing.T) {
	cards := map[int64]*model.CardDefinition{
		1: {CardNo: 1, CardName: "Fireball"},
		2: {CardNo: 2, CardName: "Shield"},
		3: {CardNo: 3, CardName: "Heal"},
	}
	cardRepo := repository.NewMockCardRepository(cards)
	pcRepo := repository.NewMockPlayerCardRepository()
	pcRepo.SeedPlayerCards("player1", []*model.PlayerCard{
		{PlayerID: "player1", CardNo: 1, ArtNo: 1, Count: 1},
		{PlayerID: "player1", CardNo: 3, ArtNo: 1, Count: 2},
	})

	svc := NewCardService(cardRepo, pcRepo)

	result, err := svc.GetAllCards(context.Background(), "player1")
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Results are sorted by CardNo from the mock repo
	assert.Equal(t, int64(1), result[0].CardNo)
	assert.True(t, result[0].IsOwned)

	assert.Equal(t, int64(2), result[1].CardNo)
	assert.False(t, result[1].IsOwned)

	assert.Equal(t, int64(3), result[2].CardNo)
	assert.True(t, result[2].IsOwned)
}

func TestGetAllCards_NoCards(t *testing.T) {
	cardRepo := repository.NewMockCardRepository(map[int64]*model.CardDefinition{})

	svc := NewCardService(cardRepo, repository.NewMockPlayerCardRepository())

	result, err := svc.GetAllCards(context.Background(), "player1")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAllCards_NoOwnedCards(t *testing.T) {
	cards := map[int64]*model.CardDefinition{
		1: {CardNo: 1, CardName: "Fireball"},
		2: {CardNo: 2, CardName: "Shield"},
	}
	cardRepo := repository.NewMockCardRepository(cards)

	svc := NewCardService(cardRepo, repository.NewMockPlayerCardRepository())

	result, err := svc.GetAllCards(context.Background(), "player1")
	require.NoError(t, err)
	require.Len(t, result, 2)

	for _, c := range result {
		assert.False(t, c.IsOwned, "card %d should not be owned", c.CardNo)
	}
}
