package service

import (
	"context"
	"fmt"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

type CardService struct {
	cardRepo repository.CardRepo
}

func NewCardService(cardRepo repository.CardRepo) *CardService {
	return &CardService{cardRepo: cardRepo}
}

func (s *CardService) GetAllCards(ctx context.Context) ([]*model.CardDefinition, error) {
	cards, err := s.cardRepo.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all cards: %w", err)
	}
	return cards, nil
}
