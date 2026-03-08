package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// ---------------------------------------------------------------------------
// MockDeckRepo — in-memory implementation of repository.DeckRepo for tests
// ---------------------------------------------------------------------------

type mockDeckRepo struct {
	playerCards map[string][]*model.PlayerCard // keyed by playerID
	decks       map[string][]*model.Deck       // keyed by playerID
	deckCards   map[int64][]model.DeckCard     // keyed by deckID
	nextDeckID  int64
}

func newMockDeckRepo() *mockDeckRepo {
	return &mockDeckRepo{
		playerCards: make(map[string][]*model.PlayerCard),
		decks:       make(map[string][]*model.Deck),
		deckCards:   make(map[int64][]model.DeckCard),
		nextDeckID:  1,
	}
}

func (m *mockDeckRepo) Create(_ context.Context, deck *model.Deck, cards []model.DeckCard) error {
	id := m.nextDeckID
	m.nextDeckID++
	deck.DeckID = id
	m.decks[deck.PlayerID] = append(m.decks[deck.PlayerID], deck)
	for i := range cards {
		cards[i].DeckID = id
	}
	m.deckCards[id] = cards
	return nil
}

func (m *mockDeckRepo) FindByPlayerID(_ context.Context, playerID string) ([]*model.Deck, error) {
	return m.decks[playerID], nil
}

func (m *mockDeckRepo) FindByID(_ context.Context, playerID string, deckID int64) (*model.Deck, error) {
	for _, d := range m.decks[playerID] {
		if d.DeckID == deckID {
			return d, nil
		}
	}
	return nil, fmt.Errorf("deck not found")
}

func (m *mockDeckRepo) GetDeckCards(_ context.Context, _ string, deckID int64) ([]model.DeckCard, error) {
	return m.deckCards[deckID], nil
}

func (m *mockDeckRepo) GetPlayerCards(_ context.Context, playerID string) ([]*model.PlayerCard, error) {
	return m.playerCards[playerID], nil
}

func (m *mockDeckRepo) Update(_ context.Context, deck *model.Deck, cards []model.DeckCard) error {
	// Replace deck in list.
	for i, d := range m.decks[deck.PlayerID] {
		if d.DeckID == deck.DeckID {
			m.decks[deck.PlayerID][i] = deck
			break
		}
	}
	m.deckCards[deck.DeckID] = cards
	return nil
}

func (m *mockDeckRepo) Delete(_ context.Context, playerID string, deckID int64) error {
	for i, d := range m.decks[playerID] {
		if d.DeckID == deckID {
			m.decks[playerID] = append(m.decks[playerID][:i], m.decks[playerID][i+1:]...)
			break
		}
	}
	delete(m.deckCards, deckID)
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// setupDeckService creates a DeckService with a mockDeckRepo and pre-injected card definitions.
// Returns the service, mock repo, and card cache.
func setupDeckService() (*DeckService, *mockDeckRepo, *cache.CardCache) {
	repo := newMockDeckRepo()
	cc := cache.NewCardCache()

	// Unlimited cards (limit = 3)
	cc.InjectForTest(1, &model.CardDefinition{
		CardNo: 1, CardName: "Compute A", Faction: "SD", CardType: "Compute",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(2, &model.CardDefinition{
		CardNo: 2, CardName: "Compute B", Faction: "SD", CardType: "Compute",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(3, &model.CardDefinition{
		CardNo: 3, CardName: "Database A", Faction: "SD", CardType: "Database",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(4, &model.CardDefinition{
		CardNo: 4, CardName: "Strategy A", Faction: "SD", CardType: "Strategy",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(5, &model.CardDefinition{
		CardNo: 5, CardName: "Incident A", Faction: "SD", CardType: "Incident",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(6, &model.CardDefinition{
		CardNo: 6, CardName: "Platform A", Faction: "SD", CardType: "Platform",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(7, &model.CardDefinition{
		CardNo: 7, CardName: "Compute C", Faction: "Tenki", CardType: "Compute",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(8, &model.CardDefinition{
		CardNo: 8, CardName: "Database B", Faction: "Tenki", CardType: "Database",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(9, &model.CardDefinition{
		CardNo: 9, CardName: "Strategy B", Faction: "Tenki", CardType: "Strategy",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})
	cc.InjectForTest(10, &model.CardDefinition{
		CardNo: 10, CardName: "Incident B", Faction: "Tenki", CardType: "Incident",
		Restriction: "unlimited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})

	// Limited card (limit = 1)
	cc.InjectForTest(50, &model.CardDefinition{
		CardNo: 50, CardName: "Limited Spell", Faction: "SD", CardType: "Strategy",
		Restriction: "limited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})

	// Semi-limited card (limit = 2)
	cc.InjectForTest(60, &model.CardDefinition{
		CardNo: 60, CardName: "SemiLimited Trap", Faction: "SD", CardType: "Incident",
		Restriction: "semi_limited", IsActive: true,
		Stats: json.RawMessage(`{}`),
	})

	svc := NewDeckService(repo, cc)
	return svc, repo, cc
}

// grantCards adds player card ownership entries to the mock repo.
func grantCards(repo *mockDeckRepo, playerID string, cards ...*model.PlayerCard) {
	for _, c := range cards {
		c.PlayerID = playerID
	}
	repo.playerCards[playerID] = append(repo.playerCards[playerID], cards...)
}

// makeEntries builds DeckCardEntry slice from (cardNo, count) pairs.
func makeEntries(pairs ...int64) []DeckCardEntry {
	var entries []DeckCardEntry
	for i := 0; i < len(pairs); i += 2 {
		entries = append(entries, DeckCardEntry{
			CardNo:              pairs[i],
			ArtNo: 0,
			Count:               int(pairs[i+1]),
		})
	}
	return entries
}

// ---------------------------------------------------------------------------
// Tests for validateDeckCards (via CreateDeck)
// ---------------------------------------------------------------------------

func TestCreateDeck_Valid30Cards(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant 3 copies of 10 distinct unlimited cards → 30 total
	for _, cardNo := range []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, ArtNo: 0, Count: 3})
	}

	// Build 30-card deck: 3 copies each of 10 cards
	entries := makeEntries(
		1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
		6, 3, 7, 3, 8, 3, 9, 3, 10, 3,
	)

	deck, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Full Deck",
		Cards:    entries,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deck.IsValid {
		t.Error("expected is_valid=true for a 30-card deck")
	}
	if deck.DeckName != "Full Deck" {
		t.Errorf("expected deck name 'Full Deck', got %q", deck.DeckName)
	}
	if len(deck.DeckCards) != 10 {
		t.Errorf("expected 10 deck_cards entries, got %d", len(deck.DeckCards))
	}
}

func TestCreateDeck_Under30Cards(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant enough cards.
	for _, cardNo := range []int64{1, 2, 3, 4, 5, 6, 7} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, ArtNo: 0, Count: 3})
	}

	// Build 20-card deck: not enough for a valid deck, but allowed with is_valid=false.
	entries := makeEntries(
		1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
		6, 3, 7, 2,
	)

	deck, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Incomplete Deck",
		Cards:    entries,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deck.IsValid {
		t.Error("expected is_valid=false for a 20-card deck")
	}
}

func TestCreateDeck_Over30Cards(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant enough cards to attempt 31.
	for _, cardNo := range []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, ArtNo: 0, Count: 3})
	}
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 1})

	entries := makeEntries(
		1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
		6, 3, 7, 3, 8, 3, 9, 3, 10, 3,
		50, 1, // total = 31
	)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Overstuffed",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for 31-card deck")
	}
	if !strings.Contains(err.Error(), "deck cannot exceed 30 cards") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_UnownedCard(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Only own card 1.
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	// Try to use card 2 which player does not own.
	entries := makeEntries(1, 3, 2, 1)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Bad Deck",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for unowned card")
	}
	if !strings.Contains(err.Error(), "not enough owned") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_UnlimitedRestriction4Copies(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant 4 copies of card 1 (unlimited, max = 3).
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 4})

	entries := makeEntries(1, 4)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Too Many Unlimited",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for 4 copies of unlimited card")
	}
	if !strings.Contains(err.Error(), "exceeds restriction limit (4/3)") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_LimitedRestriction2Copies(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant 2 copies of card 50 (limited, max = 1).
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 2})

	entries := makeEntries(50, 2)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Too Many Limited",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for 2 copies of limited card")
	}
	if !strings.Contains(err.Error(), "exceeds restriction limit (2/1)") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_SemiLimited3Copies(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant 3 copies of card 60 (semi_limited, max = 2).
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 60, ArtNo: 0, Count: 3})

	entries := makeEntries(60, 3)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Too Many SemiLimited",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for 3 copies of semi_limited card")
	}
	if !strings.Contains(err.Error(), "exceeds restriction limit (3/2)") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_ZeroCount(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	entries := []DeckCardEntry{
		{CardNo: 1, ArtNo: 0, Count: 0},
	}

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Zero Count",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for count=0")
	}
	if !strings.Contains(err.Error(), "count must be positive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_CardNotInDefinitions(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Card 999 exists in ownership but NOT in card cache.
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 999, ArtNo: 0, Count: 3})

	entries := makeEntries(999, 1)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Ghost Card",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for card not in definitions")
	}
	if !strings.Contains(err.Error(), "card 999 not found in card definitions") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests for GetPlayerCards
// ---------------------------------------------------------------------------

func TestGetPlayerCards_EnrichedWithDef(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 1},
	)

	cards, err := svc.GetPlayerCards(context.Background(), playerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 enriched cards, got %d", len(cards))
	}

	// Verify first card is enriched with definition fields.
	var found1, found50 bool
	for _, c := range cards {
		switch c.CardNo {
		case 1:
			found1 = true
			if c.CardName != "Compute A" {
				t.Errorf("card 1: expected name 'Compute A', got %q", c.CardName)
			}
			if c.Faction != "SD" {
				t.Errorf("card 1: expected faction 'SD', got %q", c.Faction)
			}
			if c.Restriction != "unlimited" {
				t.Errorf("card 1: expected restriction 'unlimited', got %q", c.Restriction)
			}
			if c.Count != 3 {
				t.Errorf("card 1: expected count=3, got %d", c.Count)
			}
		case 50:
			found50 = true
			if c.CardName != "Limited Spell" {
				t.Errorf("card 50: expected name 'Limited Spell', got %q", c.CardName)
			}
			if c.Restriction != "limited" {
				t.Errorf("card 50: expected restriction 'limited', got %q", c.Restriction)
			}
			if c.Count != 1 {
				t.Errorf("card 50: expected count=1, got %d", c.Count)
			}
		}
	}
	if !found1 {
		t.Error("card 1 not found in results")
	}
	if !found50 {
		t.Error("card 50 not found in results")
	}
}

func TestGetPlayerCards_SkipsMissingDef(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Card 1 is in cache, card 888 is NOT in cache.
	grantCards(repo, playerID,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 888, ArtNo: 0, Count: 2},
	)

	cards, err := svc.GetPlayerCards(context.Background(), playerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card (888 filtered out), got %d", len(cards))
	}
	if cards[0].CardNo != 1 {
		t.Errorf("expected card_no=1, got %d", cards[0].CardNo)
	}
}

// ---------------------------------------------------------------------------
// Tests for GetDecks
// ---------------------------------------------------------------------------

func TestGetDecks_Empty(t *testing.T) {
	svc, _, _ := setupDeckService()

	decks, err := svc.GetDecks(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decks) != 0 {
		t.Errorf("expected 0 decks, got %d", len(decks))
	}
}

func TestGetDecks_Multiple(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	for _, cardNo := range []int64{1, 2, 3} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, ArtNo: 0, Count: 3})
	}

	// Create two decks
	_, _ = svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Deck A",
		Cards:    makeEntries(1, 3, 2, 3),
	})
	_, _ = svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Deck B",
		Cards:    makeEntries(2, 3, 3, 3),
	})

	decks, err := svc.GetDecks(context.Background(), playerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decks) != 2 {
		t.Fatalf("expected 2 decks, got %d", len(decks))
	}

	// Verify DeckCards are populated
	for _, d := range decks {
		if len(d.DeckCards) == 0 {
			t.Errorf("deck %q should have DeckCards populated", d.DeckName)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for GetDeck
// ---------------------------------------------------------------------------

func TestGetDeck_Success(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 2, ArtNo: 0, Count: 3},
	)

	created, _ := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Test Deck",
		Cards:    makeEntries(1, 3, 2, 3),
	})

	deck, cards, err := svc.GetDeck(context.Background(), playerID, created.DeckID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deck.DeckName != "Test Deck" {
		t.Errorf("expected deck name 'Test Deck', got %q", deck.DeckName)
	}
	if len(cards) != 2 {
		t.Errorf("expected 2 card entries, got %d", len(cards))
	}
}

func TestGetDeck_NotFound(t *testing.T) {
	svc, _, _ := setupDeckService()

	_, _, err := svc.GetDeck(context.Background(), "p1", 999)
	if err == nil {
		t.Fatal("expected error for nonexistent deck")
	}
	if !strings.Contains(err.Error(), "deck not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests for UpdateDeck
// ---------------------------------------------------------------------------

func TestUpdateDeck_Success(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	for _, cardNo := range []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, ArtNo: 0, Count: 3})
	}

	// Create a partial deck
	created, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Original",
		Cards:    makeEntries(1, 3, 2, 3),
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.IsValid {
		t.Error("expected is_valid=false for 6-card deck")
	}

	// Update to a full 30-card deck
	fullEntries := makeEntries(
		1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
		6, 3, 7, 3, 8, 3, 9, 3, 10, 3,
	)

	updated, err := svc.UpdateDeck(context.Background(), playerID, created.DeckID, UpdateDeckRequest{
		DeckName: "Updated Full",
		Cards:    fullEntries,
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.DeckName != "Updated Full" {
		t.Errorf("expected deck name 'Updated Full', got %q", updated.DeckName)
	}
	if !updated.IsValid {
		t.Error("expected is_valid=true for 30-card deck")
	}
	if len(updated.DeckCards) != 10 {
		t.Errorf("expected 10 deck card entries, got %d", len(updated.DeckCards))
	}
}

func TestUpdateDeck_ValidationFails(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	created, _ := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Original",
		Cards:    makeEntries(1, 3),
	})

	// Try to update with unowned card
	_, err := svc.UpdateDeck(context.Background(), playerID, created.DeckID, UpdateDeckRequest{
		DeckName: "Bad Update",
		Cards:    makeEntries(1, 3, 2, 1), // card 2 not owned
	})
	if err == nil {
		t.Fatal("expected error for unowned card in update")
	}
	if !strings.Contains(err.Error(), "not enough owned") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateDeck_PlaymatAndSleeve(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	created, _ := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Original",
		Cards:    makeEntries(1, 3),
	})

	playmatNo := int64(5)
	sleeveNo := int64(3)
	updated, err := svc.UpdateDeck(context.Background(), playerID, created.DeckID, UpdateDeckRequest{
		DeckName:  "With Cosmetics",
		Cards:     makeEntries(1, 3),
		PlaymatNo: &playmatNo,
		SleeveNo:  &sleeveNo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.PlaymatNo == nil || *updated.PlaymatNo != 5 {
		t.Errorf("expected PlaymatNo=5, got %v", updated.PlaymatNo)
	}
	if updated.SleeveNo == nil || *updated.SleeveNo != 3 {
		t.Errorf("expected SleeveNo=3, got %v", updated.SleeveNo)
	}
}

// ---------------------------------------------------------------------------
// Tests for DeleteDeck
// ---------------------------------------------------------------------------

func TestDeleteDeck_Success(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	created, _ := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "To Delete",
		Cards:    makeEntries(1, 3),
	})

	err := svc.DeleteDeck(context.Background(), playerID, created.DeckID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deck is gone
	decks, _ := svc.GetDecks(context.Background(), playerID)
	if len(decks) != 0 {
		t.Errorf("expected 0 decks after delete, got %d", len(decks))
	}
}

func TestDeleteDeck_OnlyDeletesTarget(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 2, ArtNo: 0, Count: 3},
	)

	deck1, _ := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Keep",
		Cards:    makeEntries(1, 3),
	})
	deck2, _ := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Delete",
		Cards:    makeEntries(2, 3),
	})

	_ = svc.DeleteDeck(context.Background(), playerID, deck2.DeckID)

	decks, _ := svc.GetDecks(context.Background(), playerID)
	if len(decks) != 1 {
		t.Fatalf("expected 1 deck remaining, got %d", len(decks))
	}
	if decks[0].DeckID != deck1.DeckID {
		t.Errorf("expected remaining deck to be %d, got %d", deck1.DeckID, decks[0].DeckID)
	}
}

// ---------------------------------------------------------------------------
// Boundary value tests
// ---------------------------------------------------------------------------

func TestCreateDeck_29Cards_IsInvalid(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant 3 copies of cards 1-9 and 2 copies of card 10 → 9*3 + 2 = 29
	for _, cardNo := range []int64{1, 2, 3, 4, 5, 6, 7, 8, 9} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, ArtNo: 0, Count: 3})
	}
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 10, ArtNo: 0, Count: 2})

	entries := makeEntries(
		1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
		6, 3, 7, 3, 8, 3, 9, 3, 10, 2,
	)

	deck, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Almost Full",
		Cards:    entries,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deck.IsValid {
		t.Error("expected is_valid=false for a 29-card deck")
	}
}

func TestCreateDeck_NegativeCount(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	entries := []DeckCardEntry{
		{CardNo: 1, ArtNo: 0, Count: -1},
	}

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Negative Count",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for count=-1")
	}
	if !strings.Contains(err.Error(), "count must be positive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateDeck_EmptyCardsList(t *testing.T) {
	svc, _, _ := setupDeckService()
	playerID := "p1"

	deck, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Empty Deck",
		Cards:    []DeckCardEntry{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deck.IsValid {
		t.Error("expected is_valid=false for an empty deck")
	}
}

func TestCreateDeck_LimitedExactly1Copy(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 1})

	entries := makeEntries(50, 1)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Limited OK",
		Cards:    entries,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateDeck_SemiLimitedExactly2Copies(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 60, ArtNo: 0, Count: 2})

	entries := makeEntries(60, 2)

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "SemiLimited OK",
		Cards:    entries,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ArtNo cross-variant test
// ---------------------------------------------------------------------------

func TestCreateDeck_DifferentArtNo(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant card 1 with two art variants: ArtNo 0 (count=3) and ArtNo 1 (count=3).
	grantCards(repo, playerID,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 1, ArtNo: 1, Count: 3},
	)

	// Use 3 of variant 0 + 1 of variant 1 = 4 total for cardNo 1.
	// Restriction is "unlimited" (max 3), so this should fail.
	entries := []DeckCardEntry{
		{CardNo: 1, ArtNo: 0, Count: 3},
		{CardNo: 1, ArtNo: 1, Count: 1},
	}

	_, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Cross Variant",
		Cards:    entries,
	})
	if err == nil {
		t.Fatal("expected error for exceeding restriction across art variants")
	}
	if !strings.Contains(err.Error(), "exceeds restriction limit (4/3)") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Error path tests
// ---------------------------------------------------------------------------

type errorFindByIDRepo struct {
	*mockDeckRepo
}

func (r *errorFindByIDRepo) FindByID(_ context.Context, _ string, _ int64) (*model.Deck, error) {
	return nil, fmt.Errorf("db connection lost")
}

func TestGetDeck_RepoFindByIDError(t *testing.T) {
	repo := newMockDeckRepo()
	cc := cache.NewCardCache()
	errRepo := &errorFindByIDRepo{mockDeckRepo: repo}
	svc := NewDeckService(errRepo, cc)

	_, _, err := svc.GetDeck(context.Background(), "p1", 1)
	if err == nil {
		t.Fatal("expected error from FindByID")
	}
	if !strings.Contains(err.Error(), "find deck:") {
		t.Errorf("expected error wrapped with 'find deck:', got: %v", err)
	}
	if !strings.Contains(err.Error(), "db connection lost") {
		t.Errorf("expected underlying error 'db connection lost', got: %v", err)
	}
}

func TestCreateDeck_DeckIDAssigned(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	deck, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "ID Check",
		Cards:    makeEntries(1, 3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deck.DeckID <= 0 {
		t.Errorf("expected DeckID > 0, got %d", deck.DeckID)
	}
}

func TestUpdateDeck_VerifyPersistence(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	grantCards(repo, playerID,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 2, ArtNo: 0, Count: 3},
	)

	created, err := svc.CreateDeck(context.Background(), playerID, CreateDeckRequest{
		DeckName: "Before Update",
		Cards:    makeEntries(1, 3),
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, err = svc.UpdateDeck(context.Background(), playerID, created.DeckID, UpdateDeckRequest{
		DeckName: "After Update",
		Cards:    makeEntries(1, 3, 2, 3),
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Verify persistence via GetDeck.
	deck, cards, err := svc.GetDeck(context.Background(), playerID, created.DeckID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if deck.DeckName != "After Update" {
		t.Errorf("expected deck name 'After Update', got %q", deck.DeckName)
	}
	if len(cards) != 2 {
		t.Errorf("expected 2 card entries after update, got %d", len(cards))
	}
}

// ---------------------------------------------------------------------------
// RestrictionCopyCount tests
// ---------------------------------------------------------------------------

func TestRestrictionCopyCount(t *testing.T) {
	tests := []struct {
		restriction string
		expected    int
	}{
		{"unlimited", 3},
		{"semi_limited", 2},
		{"limited", 1},
		{"", 3},
	}
	for _, tt := range tests {
		got := restrictionCopyCount(tt.restriction)
		if got != tt.expected {
			t.Errorf("restrictionCopyCount(%q) = %d, want %d", tt.restriction, got, tt.expected)
		}
	}
}
