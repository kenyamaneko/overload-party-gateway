package service

import (
	"context"
	"fmt"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

type DeckService struct {
	deckRepo       port.DeckRepo
	playerCardRepo port.PlayerCardRepo
	cardCache      *cache.CardCache
}

func NewDeckService(deckRepo port.DeckRepo, playerCardRepo port.PlayerCardRepo, cardCache *cache.CardCache) *DeckService {
	return &DeckService{deckRepo: deckRepo, playerCardRepo: playerCardRepo, cardCache: cardCache}
}

func (s *DeckService) GetDecks(ctx context.Context, playerID string) ([]*model.Deck, error) {
	decks, err := s.deckRepo.FindByPlayerID(ctx, playerID)
	if err != nil {
		return nil, err
	}

	ownedCards, err := s.playerCardRepo.GetPlayerCards(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get owned cards: %w", err)
	}

	for _, d := range decks {
		if err := s.populateDeckCards(ctx, d); err != nil {
			return nil, err
		}
		d.IsValid = s.computeIsValid(d.DeckCards, ownedCards)
	}
	return decks, nil
}

func (s *DeckService) GetDeck(ctx context.Context, playerID string, deckID int64) (*model.Deck, []model.DeckCard, error) {
	deck, err := s.deckRepo.FindByID(ctx, playerID, deckID)
	if err != nil {
		return nil, nil, fmt.Errorf("find deck: %w", err)
	}

	cards, err := s.deckRepo.GetDeckCards(ctx, playerID, deckID)
	if err != nil {
		return nil, nil, fmt.Errorf("get deck cards: %w", err)
	}

	return deck, cards, nil
}

// DeckCardEntry represents a card entry in a deck create/update request.
type DeckCardEntry struct {
	CardID string `json:"card_id"`
	ArtNo  int64  `json:"art_no"`
	Count  int    `json:"count"`
}

type CreateDeckRequest struct {
	DeckName  string          `json:"deck_name" binding:"required"`
	Cards     []DeckCardEntry `json:"cards" binding:"required"`
	PlaymatNo *int64          `json:"playmat_no,omitempty"`
	SleeveNo  *int64          `json:"sleeve_no,omitempty"`
}

func (s *DeckService) CreateDeck(ctx context.Context, playerID string, req CreateDeckRequest) (*model.Deck, error) {
	ownedCards, err := s.playerCardRepo.GetPlayerCards(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get owned cards: %w", err)
	}

	if err := s.validateDeckCards(req.Cards, ownedCards); err != nil {
		return nil, err
	}

	totalCards := 0
	for _, c := range req.Cards {
		totalCards += c.Count
	}

	now := time.Now()
	deck := &model.Deck{
		PlayerID:  playerID,
		DeckName:  req.DeckName,
		IsValid:   totalCards == constants.DeckSize,
		PlaymatNo: req.PlaymatNo,
		SleeveNo:  req.SleeveNo,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// DeckID will be set by repo.Create (DB-generated BIGSERIAL)
	var deckCards []model.DeckCard
	for _, entry := range req.Cards {
		deckCards = append(deckCards, model.DeckCard{
			PlayerID: playerID,
			CardID:   entry.CardID,
			ArtNo:    entry.ArtNo,
			Count:    entry.Count,
		})
	}

	if err := s.deckRepo.Create(ctx, deck, deckCards); err != nil {
		return nil, fmt.Errorf("create deck: %w", err)
	}

	if err := s.populateDeckCards(ctx, deck); err != nil {
		return nil, err
	}
	return deck, nil
}

type UpdateDeckRequest struct {
	DeckName  string          `json:"deck_name" binding:"required"`
	Cards     []DeckCardEntry `json:"cards" binding:"required"`
	PlaymatNo *int64          `json:"playmat_no,omitempty"`
	SleeveNo  *int64          `json:"sleeve_no,omitempty"`
}

func (s *DeckService) UpdateDeck(ctx context.Context, playerID string, deckID int64, req UpdateDeckRequest) (*model.Deck, error) {
	ownedCards, err := s.playerCardRepo.GetPlayerCards(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get owned cards: %w", err)
	}

	if err := s.validateDeckCards(req.Cards, ownedCards); err != nil {
		return nil, err
	}

	totalCards := 0
	for _, c := range req.Cards {
		totalCards += c.Count
	}

	deck := &model.Deck{
		PlayerID:  playerID,
		DeckID:    deckID,
		DeckName:  req.DeckName,
		IsValid:   totalCards == constants.DeckSize,
		PlaymatNo: req.PlaymatNo,
		SleeveNo:  req.SleeveNo,
		UpdatedAt: time.Now(),
	}

	var deckCards []model.DeckCard
	for _, entry := range req.Cards {
		deckCards = append(deckCards, model.DeckCard{
			PlayerID: playerID,
			DeckID:   deckID,
			CardID:   entry.CardID,
			ArtNo:    entry.ArtNo,
			Count:    entry.Count,
		})
	}

	if err := s.deckRepo.Update(ctx, deck, deckCards); err != nil {
		return nil, fmt.Errorf("update deck: %w", err)
	}

	if err := s.populateDeckCards(ctx, deck); err != nil {
		return nil, err
	}
	return deck, nil
}

// ValidateDeckForBattle checks that a deck is battle-ready:
// exactly DeckSize cards, all cards owned, and all restriction limits respected.
func (s *DeckService) ValidateDeckForBattle(ctx context.Context, playerID string, deckID int64) error {
	deckCards, err := s.deckRepo.GetDeckCards(ctx, playerID, deckID)
	if err != nil {
		return fmt.Errorf("get deck cards: %w", err)
	}

	// Convert DeckCard → DeckCardEntry for reuse of validateDeckCards.
	entries := make([]DeckCardEntry, len(deckCards))
	for i, dc := range deckCards {
		entries[i] = DeckCardEntry{CardID: dc.CardID, ArtNo: dc.ArtNo, Count: dc.Count}
	}

	// Check total card count is exactly DeckSize.
	totalCards := 0
	for _, e := range entries {
		totalCards += e.Count
	}
	if totalCards != constants.DeckSize {
		return fmt.Errorf("deck has %d cards, need exactly %d", totalCards, constants.DeckSize)
	}

	ownedCards, err := s.playerCardRepo.GetPlayerCards(ctx, playerID)
	if err != nil {
		return fmt.Errorf("get owned cards: %w", err)
	}

	return s.validateDeckCards(entries, ownedCards)
}

func (s *DeckService) DeleteDeck(ctx context.Context, playerID string, deckID int64) error {
	return s.deckRepo.Delete(ctx, playerID, deckID)
}

// populateDeckCards fills deck.DeckCards with the card composition (card_id + art_no + count).
func (s *DeckService) populateDeckCards(ctx context.Context, deck *model.Deck) error {
	cards, err := s.deckRepo.GetDeckCards(ctx, deck.PlayerID, deck.DeckID)
	if err != nil {
		return fmt.Errorf("get deck cards for deck %d: %w", deck.DeckID, err)
	}
	deck.DeckCards = cards
	return nil
}

func (s *DeckService) GetPlayerCards(ctx context.Context, playerID string) ([]*model.PlayerCardWithDef, error) {
	pcs, err := s.playerCardRepo.GetPlayerCards(ctx, playerID)
	if err != nil {
		return nil, err
	}

	result := make([]*model.PlayerCardWithDef, 0, len(pcs))
	for _, pc := range pcs {
		cd := s.cardCache.Get(pc.CardID)
		if cd == nil {
			continue // skip cards not in cache (deleted/inactive)
		}
		result = append(result, &model.PlayerCardWithDef{
			CardID:      pc.CardID,
			ArtNo:       pc.ArtNo,
			Count:       pc.Count,
			CardName:    cd.CardName,
			Faction:     cd.Faction,
			CardType:    cd.CardType,
			Resizable:   cd.Resizable,
			Elastic:     cd.Elastic,
			Stats:       cd.Stats,
			EffectText:  cd.EffectText,
			Restriction: cd.Restriction,
		})
	}
	return result, nil
}

// ownedKey returns a key for the (card_id, art_no) pair.
type ownedKey struct {
	cardID  string
	variant int64
}

func (s *DeckService) validateDeckCards(entries []DeckCardEntry, ownedCards []*model.PlayerCard) error {
	// Total card count check.
	totalCards := 0
	for _, e := range entries {
		if e.Count <= 0 {
			return fmt.Errorf("card %s variant %d: count must be positive", e.CardID, e.ArtNo)
		}
		totalCards += e.Count
	}
	if totalCards > constants.DeckSize {
		return fmt.Errorf("deck cannot exceed %d cards", constants.DeckSize)
	}

	// Build owned map: (card_id, art_no) → count.
	owned := make(map[ownedKey]int, len(ownedCards))
	for _, c := range ownedCards {
		owned[ownedKey{c.CardID, c.ArtNo}] = c.Count
	}

	// Ownership check: each (card_id, variant) count ≤ owned count.
	for _, e := range entries {
		key := ownedKey{e.CardID, e.ArtNo}
		if owned[key] < e.Count {
			return fmt.Errorf("card %s variant %d: not enough owned (need %d, have %d)",
				e.CardID, e.ArtNo, e.Count, owned[key])
		}
	}

	// Restriction check: total count per card_id ≤ restriction limit.
	cardIDTotals := make(map[string]int)
	for _, e := range entries {
		cardIDTotals[e.CardID] += e.Count
	}
	for cardID, total := range cardIDTotals {
		card := s.cardCache.Get(cardID)
		if card == nil {
			return fmt.Errorf("card %s not found in card definitions", cardID)
		}
		limit := restrictionCopyCount(card.Restriction)
		if total > limit {
			return fmt.Errorf("card %s (%s): exceeds restriction limit (%d/%d)",
				cardID, card.Restriction, total, limit)
		}
	}

	return nil
}

// computeIsValid checks whether a deck's cards satisfy all battle-readiness
// conditions: exactly DeckSize total, all owned, and within restriction limits.
func (s *DeckService) computeIsValid(deckCards []model.DeckCard, ownedCards []*model.PlayerCard) bool {
	totalCards := 0
	for _, dc := range deckCards {
		totalCards += dc.Count
	}
	if totalCards != constants.DeckSize {
		return false
	}

	entries := make([]DeckCardEntry, len(deckCards))
	for i, dc := range deckCards {
		entries[i] = DeckCardEntry{CardID: dc.CardID, ArtNo: dc.ArtNo, Count: dc.Count}
	}
	return s.validateDeckCards(entries, ownedCards) == nil
}

func restrictionCopyCount(restriction string) int {
	switch restriction {
	case "semi_limited":
		return 2
	case "limited":
		return 1
	default:
		return 3
	}
}
