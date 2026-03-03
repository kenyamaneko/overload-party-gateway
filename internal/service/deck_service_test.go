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
	decks       map[string][]*model.Deck      // keyed by playerID
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

func (m *mockDeckRepo) GetDeckCardNos(_ context.Context, _ string, deckID int64) ([]int64, error) {
	var nos []int64
	for _, dc := range m.deckCards[deckID] {
		for i := 0; i < dc.Count; i++ {
			nos = append(nos, dc.CardNo)
		}
	}
	return nos, nil
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
			IllustrationVariant: 0,
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
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, IllustrationVariant: 0, Count: 3})
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
	if len(deck.CardNos) != 30 {
		t.Errorf("expected 30 card_nos, got %d", len(deck.CardNos))
	}
}

func TestCreateDeck_Under30Cards(t *testing.T) {
	svc, repo, _ := setupDeckService()
	playerID := "p1"

	// Grant enough cards.
	for _, cardNo := range []int64{1, 2, 3, 4, 5, 6, 7} {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, IllustrationVariant: 0, Count: 3})
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
		grantCards(repo, playerID, &model.PlayerCard{CardNo: cardNo, IllustrationVariant: 0, Count: 3})
	}
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 50, IllustrationVariant: 0, Count: 1})

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
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, IllustrationVariant: 0, Count: 3})

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
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, IllustrationVariant: 0, Count: 4})

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
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 50, IllustrationVariant: 0, Count: 2})

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
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 60, IllustrationVariant: 0, Count: 3})

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

	grantCards(repo, playerID, &model.PlayerCard{CardNo: 1, IllustrationVariant: 0, Count: 3})

	entries := []DeckCardEntry{
		{CardNo: 1, IllustrationVariant: 0, Count: 0},
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
	grantCards(repo, playerID, &model.PlayerCard{CardNo: 999, IllustrationVariant: 0, Count: 3})

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
		&model.PlayerCard{CardNo: 1, IllustrationVariant: 0, Count: 3},
		&model.PlayerCard{CardNo: 50, IllustrationVariant: 0, Count: 1},
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
		&model.PlayerCard{CardNo: 1, IllustrationVariant: 0, Count: 3},
		&model.PlayerCard{CardNo: 888, IllustrationVariant: 0, Count: 2},
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
