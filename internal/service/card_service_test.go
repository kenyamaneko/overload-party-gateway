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
	cards := map[string]*model.CardDefinition{
		"C-001": {CardID: "C-001", CardName: "Fireball"},
		"C-002": {CardID: "C-002", CardName: "Shield"},
		"C-003": {CardID: "C-003", CardName: "Heal"},
	}
	cardRepo := repository.NewMockCardRepository(cards)
	pcRepo := repository.NewMockPlayerCardRepository()
	pcRepo.SeedPlayerCards("player1", []*model.PlayerCard{
		{PlayerID: "player1", CardID: "C-001", ArtNo: 1, Count: 1},
		{PlayerID: "player1", CardID: "C-003", ArtNo: 1, Count: 2},
	})

	svc := NewCardService(cardRepo, pcRepo)

	result, err := svc.GetAllCards(context.Background(), "player1")
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Results are sorted by CardID from the mock repo
	assert.Equal(t, "C-001", result[0].CardID)
	assert.True(t, result[0].IsOwned)

	assert.Equal(t, "C-002", result[1].CardID)
	assert.False(t, result[1].IsOwned)

	assert.Equal(t, "C-003", result[2].CardID)
	assert.True(t, result[2].IsOwned)
}

func TestGetAllCards_NoCards(t *testing.T) {
	cardRepo := repository.NewMockCardRepository(map[string]*model.CardDefinition{})

	svc := NewCardService(cardRepo, repository.NewMockPlayerCardRepository())

	result, err := svc.GetAllCards(context.Background(), "player1")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAllCards_NoOwnedCards(t *testing.T) {
	cards := map[string]*model.CardDefinition{
		"C-001": {CardID: "C-001", CardName: "Fireball"},
		"C-002": {CardID: "C-002", CardName: "Shield"},
	}
	cardRepo := repository.NewMockCardRepository(cards)

	svc := NewCardService(cardRepo, repository.NewMockPlayerCardRepository())

	result, err := svc.GetAllCards(context.Background(), "player1")
	require.NoError(t, err)
	require.Len(t, result, 2)

	for _, c := range result {
		assert.False(t, c.IsOwned, "card %s should not be owned", c.CardID)
	}
}
