package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	gencache "github.com/kenyamaneko/overload-party-common/packages/devdata/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// newDevPlayerSetup returns a callback that gives a newly created dev player
// all active cards and the starter decks from devdata.
func newDevPlayerSetup(cardCache *cache.CardCache, playerCardRepo *repository.MockPlayerCardRepository, deckRepo *repository.MockDeckRepository) func(ctx context.Context, playerID string) error {
	return func(ctx context.Context, playerID string) error {
		seedAllCards(playerID, cardCache, playerCardRepo)

		if err := seedStarterDecks(ctx, playerID, deckRepo); err != nil {
			return err
		}
		return nil
	}
}

// seedAllCards gives the player 3 copies of every active card.
func seedAllCards(playerID string, cardCache *cache.CardCache, repo *repository.MockPlayerCardRepository) {
	var playerCards []*model.PlayerCard
	for _, card := range cardCache.All() {
		if !card.IsActive {
			continue
		}
		playerCards = append(playerCards, &model.PlayerCard{
			PlayerID: playerID,
			CardID:   card.CardID,
			ArtNo:    0,
			Count:    3,
		})
	}
	repo.SeedPlayerCards(playerID, playerCards)
}

// seedStarterDecks creates the starter decks defined in starter_decks_gen.json.
func seedStarterDecks(ctx context.Context, playerID string, deckRepo *repository.MockDeckRepository) error {
	var starterDecks []struct {
		DeckName string   `json:"deck_name"`
		Cards    []string `json:"cards"`
	}
	if err := json.Unmarshal(gencache.StarterDecksJSON, &starterDecks); err != nil {
		return fmt.Errorf("unmarshal starter_decks_gen.json: %w", err)
	}

	for _, sd := range starterDecks {
		cardCounts := make(map[string]int)
		for _, cardID := range sd.Cards {
			cardCounts[cardID]++
		}

		var deckCards []model.DeckCard
		for cardID, count := range cardCounts {
			deckCards = append(deckCards, model.DeckCard{
				PlayerID: playerID,
				CardID:   cardID,
				ArtNo:    0,
				Count:    count,
			})
		}

		deck := &model.Deck{
			PlayerID:  playerID,
			DeckName:  sd.DeckName,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := deckRepo.Create(ctx, deck, deckCards); err != nil {
			return err
		}
		log.Printf("auto-created starter deck %s for %s: %d cards, deckID=%d", sd.DeckName, playerID, len(sd.Cards), deck.DeckID)
	}
	return nil
}

// seedNewsMock populates the mock news repository from embedded JSON.
func seedNewsMock(repo *repository.MockNewsRepository) {
	var articles []*model.NewsArticle
	if err := json.Unmarshal(gencache.NewsJSON, &articles); err != nil {
		log.Fatalf("failed to unmarshal news mock: %v", err)
	}
	now := time.Now()
	for _, a := range articles {
		a.FetchedAt = now
	}
	repo.Seed(articles)
	log.Printf("seeded %d news articles from mock JSON", len(articles))
}
