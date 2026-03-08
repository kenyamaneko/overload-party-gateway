package service

import (
	"context"
	"fmt"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

type CardService struct {
	cardRepo repository.CardRepo
	deckRepo repository.DeckRepo
}

func NewCardService(cardRepo repository.CardRepo, deckRepo repository.DeckRepo) *CardService {
	return &CardService{cardRepo: cardRepo, deckRepo: deckRepo}
}

type CardWithOwnership struct {
	*model.CardDefinition
	IsOwned bool `json:"is_owned"`
}

func (s *CardService) GetAllCards(ctx context.Context, playerID string) ([]*CardWithOwnership, error) {
	cards, err := s.cardRepo.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all cards: %w", err)
	}

	playerCards, err := s.deckRepo.GetPlayerCards(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get player cards: %w", err)
	}

	owned := make(map[int64]bool, len(playerCards))
	for _, pc := range playerCards {
		owned[pc.CardNo] = true
	}

	result := make([]*CardWithOwnership, len(cards))
	for i, card := range cards {
		result[i] = &CardWithOwnership{
			CardDefinition: card,
			IsOwned:        owned[card.CardNo],
		}
	}
	return result, nil
}
