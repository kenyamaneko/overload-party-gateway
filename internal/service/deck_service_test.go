package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
func setupDeckService() (*DeckService, *mockDeckRepo, *cache.CardCache) {
	repo := newMockDeckRepo()
	cc := cache.NewCardCache()

	// Unlimited cards (limit = 3)
	unlimitedCards := []struct {
		no      int64
		name    string
		faction string
		typ     string
	}{
		{1, "Compute A", "SD", "Compute"},
		{2, "Compute B", "SD", "Compute"},
		{3, "Database A", "SD", "Database"},
		{4, "Strategy A", "SD", "Strategy"},
		{5, "Incident A", "SD", "Incident"},
		{6, "Platform A", "SD", "Platform"},
		{7, "Compute C", "Tenki", "Compute"},
		{8, "Database B", "Tenki", "Database"},
		{9, "Strategy B", "Tenki", "Strategy"},
		{10, "Incident B", "Tenki", "Incident"},
	}
	for _, c := range unlimitedCards {
		cc.InjectForTest(c.no, &model.CardDefinition{
			CardNo: c.no, CardName: c.name, Faction: c.faction, CardType: c.typ,
			Restriction: "unlimited", IsActive: true,
			Stats: json.RawMessage(`{}`),
		})
	}

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
			CardNo: pairs[i],
			ArtNo:  0,
			Count:  int(pairs[i+1]),
		})
	}
	return entries
}

// grantUnlimited grants 3 copies each of the given card numbers.
func grantUnlimited(repo *mockDeckRepo, playerID string, cardNos ...int64) {
	for _, no := range cardNos {
		grantCards(repo, playerID, &model.PlayerCard{CardNo: no, ArtNo: 0, Count: 3})
	}
}

// allTenCards is a convenience for granting/building 10 distinct cards.
var allTenCards = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

// full30Entries returns entries for a valid 30-card deck (3 copies × 10 cards).
func full30Entries() []DeckCardEntry {
	return makeEntries(
		1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
		6, 3, 7, 3, 8, 3, 9, 3, 10, 3,
	)
}

// ---------------------------------------------------------------------------
// Tests for CreateDeck — validity (is_valid flag)
// ---------------------------------------------------------------------------

func TestCreateDeck_Validity(t *testing.T) {
	tests := []struct {
		name      string
		deckName  string
		grant     func(repo *mockDeckRepo, pid string)
		entries   []DeckCardEntry
		wantValid bool
	}{
		{
			name:     "30 cards → valid",
			deckName: "Full Deck",
			grant: func(repo *mockDeckRepo, pid string) {
				grantUnlimited(repo, pid, allTenCards...)
			},
			entries:   full30Entries(),
			wantValid: true,
		},
		{
			name:     "20 cards → invalid",
			deckName: "Incomplete Deck",
			grant: func(repo *mockDeckRepo, pid string) {
				grantUnlimited(repo, pid, 1, 2, 3, 4, 5, 6, 7)
			},
			entries:   makeEntries(1, 3, 2, 3, 3, 3, 4, 3, 5, 3, 6, 3, 7, 2),
			wantValid: false,
		},
		{
			name:     "29 cards → invalid",
			deckName: "Almost Full",
			grant: func(repo *mockDeckRepo, pid string) {
				grantUnlimited(repo, pid, 1, 2, 3, 4, 5, 6, 7, 8, 9)
				grantCards(repo, pid, &model.PlayerCard{CardNo: 10, ArtNo: 0, Count: 2})
			},
			entries: makeEntries(
				1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
				6, 3, 7, 3, 8, 3, 9, 3, 10, 2,
			),
			wantValid: false,
		},
		{
			name:     "0 cards → invalid",
			deckName: "Empty Deck",
			grant:    func(repo *mockDeckRepo, pid string) {},
			entries:  []DeckCardEntry{},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := setupDeckService()
			pid := "p1"
			tt.grant(repo, pid)

			deck, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
				DeckName: tt.deckName,
				Cards:    tt.entries,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantValid, deck.IsValid)
			assert.Equal(t, tt.deckName, deck.DeckName)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for CreateDeck — validation errors
// ---------------------------------------------------------------------------

func TestCreateDeck_ValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		grant      func(repo *mockDeckRepo, pid string)
		entries    []DeckCardEntry
		wantErrMsg string
	}{
		{
			name: "31 cards exceeds max",
			grant: func(repo *mockDeckRepo, pid string) {
				grantUnlimited(repo, pid, allTenCards...)
				grantCards(repo, pid, &model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 1})
			},
			entries: makeEntries(
				1, 3, 2, 3, 3, 3, 4, 3, 5, 3,
				6, 3, 7, 3, 8, 3, 9, 3, 10, 3,
				50, 1,
			),
			wantErrMsg: "deck cannot exceed 30 cards",
		},
		{
			name: "unowned card",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})
			},
			entries:    makeEntries(1, 3, 2, 1),
			wantErrMsg: "not enough owned",
		},
		{
			name: "unlimited card 4 copies (max 3)",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 4})
			},
			entries:    makeEntries(1, 4),
			wantErrMsg: "exceeds restriction limit (4/3)",
		},
		{
			name: "limited card 2 copies (max 1)",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 2})
			},
			entries:    makeEntries(50, 2),
			wantErrMsg: "exceeds restriction limit (2/1)",
		},
		{
			name: "semi_limited card 3 copies (max 2)",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 60, ArtNo: 0, Count: 3})
			},
			entries:    makeEntries(60, 3),
			wantErrMsg: "exceeds restriction limit (3/2)",
		},
		{
			name: "count = 0",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})
			},
			entries:    []DeckCardEntry{{CardNo: 1, ArtNo: 0, Count: 0}},
			wantErrMsg: "count must be positive",
		},
		{
			name: "count = -1",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})
			},
			entries:    []DeckCardEntry{{CardNo: 1, ArtNo: 0, Count: -1}},
			wantErrMsg: "count must be positive",
		},
		{
			name: "card not in definitions",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid, &model.PlayerCard{CardNo: 999, ArtNo: 0, Count: 3})
			},
			entries:    makeEntries(999, 1),
			wantErrMsg: "card 999 not found in card definitions",
		},
		{
			name: "cross-variant exceeds restriction",
			grant: func(repo *mockDeckRepo, pid string) {
				grantCards(repo, pid,
					&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
					&model.PlayerCard{CardNo: 1, ArtNo: 1, Count: 3},
				)
			},
			entries: []DeckCardEntry{
				{CardNo: 1, ArtNo: 0, Count: 3},
				{CardNo: 1, ArtNo: 1, Count: 1}, // total 4, max 3
			},
			wantErrMsg: "exceeds restriction limit (4/3)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := setupDeckService()
			pid := "p1"
			tt.grant(repo, pid)

			_, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
				DeckName: "Test",
				Cards:    tt.entries,
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for CreateDeck — restriction boundary (exactly at limit should pass)
// ---------------------------------------------------------------------------

func TestCreateDeck_RestrictionAtLimit(t *testing.T) {
	tests := []struct {
		name    string
		cardNo  int64
		count   int
	}{
		{"limited 1 copy (max 1)", 50, 1},
		{"semi_limited 2 copies (max 2)", 60, 2},
		{"unlimited 3 copies (max 3)", 1, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := setupDeckService()
			pid := "p1"
			grantCards(repo, pid, &model.PlayerCard{CardNo: tt.cardNo, ArtNo: 0, Count: tt.count})

			_, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
				DeckName: "OK Deck",
				Cards:    makeEntries(tt.cardNo, int64(tt.count)),
			})

			require.NoError(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for CreateDeck — misc
// ---------------------------------------------------------------------------

func TestCreateDeck_DeckIDAssigned(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	deck, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "ID Check",
		Cards:    makeEntries(1, 3),
	})

	require.NoError(t, err)
	assert.Greater(t, deck.DeckID, int64(0))
}

func TestCreateDeck_DeckCardsPopulated(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantUnlimited(repo, pid, allTenCards...)

	deck, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Full Deck",
		Cards:    full30Entries(),
	})

	require.NoError(t, err)
	assert.Len(t, deck.DeckCards, 10)
}

// ---------------------------------------------------------------------------
// Tests for GetPlayerCards
// ---------------------------------------------------------------------------

func TestGetPlayerCards_EnrichedWithDef(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantCards(repo, pid,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 50, ArtNo: 0, Count: 1},
	)

	cards, err := svc.GetPlayerCards(context.Background(), pid)
	require.NoError(t, err)
	require.Len(t, cards, 2)

	// Build a map for easy lookup.
	byNo := map[int64]*model.PlayerCardWithDef{}
	for _, c := range cards {
		byNo[c.CardNo] = c
	}

	assert.Equal(t, "Compute A", byNo[1].CardName)
	assert.Equal(t, "SD", byNo[1].Faction)
	assert.Equal(t, "unlimited", byNo[1].Restriction)
	assert.Equal(t, 3, byNo[1].Count)

	assert.Equal(t, "Limited Spell", byNo[50].CardName)
	assert.Equal(t, "limited", byNo[50].Restriction)
	assert.Equal(t, 1, byNo[50].Count)
}

func TestGetPlayerCards_SkipsMissingDef(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantCards(repo, pid,
		&model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3},
		&model.PlayerCard{CardNo: 888, ArtNo: 0, Count: 2}, // not in cache
	)

	cards, err := svc.GetPlayerCards(context.Background(), pid)

	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, int64(1), cards[0].CardNo)
}

// ---------------------------------------------------------------------------
// Tests for GetDecks
// ---------------------------------------------------------------------------

func TestGetDecks_Empty(t *testing.T) {
	svc, _, _ := setupDeckService()

	decks, err := svc.GetDecks(context.Background(), "p1")

	require.NoError(t, err)
	assert.Empty(t, decks)
}

func TestGetDecks_Multiple(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantUnlimited(repo, pid, 1, 2, 3)

	_, _ = svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Deck A", Cards: makeEntries(1, 3, 2, 3),
	})
	_, _ = svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Deck B", Cards: makeEntries(2, 3, 3, 3),
	})

	decks, err := svc.GetDecks(context.Background(), pid)

	require.NoError(t, err)
	require.Len(t, decks, 2)
	for _, d := range decks {
		assert.NotEmpty(t, d.DeckCards, "deck %q should have DeckCards populated", d.DeckName)
	}
}

// ---------------------------------------------------------------------------
// Tests for GetDeck
// ---------------------------------------------------------------------------

func TestGetDeck_Success(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantUnlimited(repo, pid, 1, 2)

	created, _ := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Test Deck", Cards: makeEntries(1, 3, 2, 3),
	})

	deck, cards, err := svc.GetDeck(context.Background(), pid, created.DeckID)

	require.NoError(t, err)
	assert.Equal(t, "Test Deck", deck.DeckName)
	assert.Len(t, cards, 2)
}

func TestGetDeck_NotFound(t *testing.T) {
	svc, _, _ := setupDeckService()

	_, _, err := svc.GetDeck(context.Background(), "p1", 999)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deck not found")
}

func TestGetDeck_RepoFindByIDError(t *testing.T) {
	repo := newMockDeckRepo()
	cc := cache.NewCardCache()
	errRepo := &errorFindByIDRepo{mockDeckRepo: repo}
	svc := NewDeckService(errRepo, cc)

	_, _, err := svc.GetDeck(context.Background(), "p1", 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "find deck:")
	assert.Contains(t, err.Error(), "db connection lost")
}

type errorFindByIDRepo struct {
	*mockDeckRepo
}

func (r *errorFindByIDRepo) FindByID(_ context.Context, _ string, _ int64) (*model.Deck, error) {
	return nil, fmt.Errorf("db connection lost")
}

// ---------------------------------------------------------------------------
// Tests for UpdateDeck
// ---------------------------------------------------------------------------

func TestUpdateDeck_Success(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantUnlimited(repo, pid, allTenCards...)

	// Create a partial deck (6 cards → invalid)
	created, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Original", Cards: makeEntries(1, 3, 2, 3),
	})
	require.NoError(t, err)
	assert.False(t, created.IsValid)

	// Update to a full 30-card deck
	updated, err := svc.UpdateDeck(context.Background(), pid, created.DeckID, UpdateDeckRequest{
		DeckName: "Updated Full", Cards: full30Entries(),
	})

	require.NoError(t, err)
	assert.Equal(t, "Updated Full", updated.DeckName)
	assert.True(t, updated.IsValid)
	assert.Len(t, updated.DeckCards, 10)
}

func TestUpdateDeck_ValidationFails(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	created, _ := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Original", Cards: makeEntries(1, 3),
	})

	_, err := svc.UpdateDeck(context.Background(), pid, created.DeckID, UpdateDeckRequest{
		DeckName: "Bad Update", Cards: makeEntries(1, 3, 2, 1), // card 2 not owned
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enough owned")
}

func TestUpdateDeck_PlaymatAndSleeve(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	created, _ := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Original", Cards: makeEntries(1, 3),
	})

	playmatNo := int64(5)
	sleeveNo := int64(3)
	updated, err := svc.UpdateDeck(context.Background(), pid, created.DeckID, UpdateDeckRequest{
		DeckName: "With Cosmetics", Cards: makeEntries(1, 3),
		PlaymatNo: &playmatNo, SleeveNo: &sleeveNo,
	})

	require.NoError(t, err)
	require.NotNil(t, updated.PlaymatNo)
	assert.Equal(t, int64(5), *updated.PlaymatNo)
	require.NotNil(t, updated.SleeveNo)
	assert.Equal(t, int64(3), *updated.SleeveNo)
}

func TestUpdateDeck_VerifyPersistence(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantUnlimited(repo, pid, 1, 2)

	created, err := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Before Update", Cards: makeEntries(1, 3),
	})
	require.NoError(t, err)

	_, err = svc.UpdateDeck(context.Background(), pid, created.DeckID, UpdateDeckRequest{
		DeckName: "After Update", Cards: makeEntries(1, 3, 2, 3),
	})
	require.NoError(t, err)

	deck, cards, err := svc.GetDeck(context.Background(), pid, created.DeckID)

	require.NoError(t, err)
	assert.Equal(t, "After Update", deck.DeckName)
	assert.Len(t, cards, 2)
}

// ---------------------------------------------------------------------------
// Tests for DeleteDeck
// ---------------------------------------------------------------------------

func TestDeleteDeck_Success(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantCards(repo, pid, &model.PlayerCard{CardNo: 1, ArtNo: 0, Count: 3})

	created, _ := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "To Delete", Cards: makeEntries(1, 3),
	})

	err := svc.DeleteDeck(context.Background(), pid, created.DeckID)

	require.NoError(t, err)
	decks, _ := svc.GetDecks(context.Background(), pid)
	assert.Empty(t, decks)
}

func TestDeleteDeck_OnlyDeletesTarget(t *testing.T) {
	svc, repo, _ := setupDeckService()
	pid := "p1"
	grantUnlimited(repo, pid, 1, 2)

	deck1, _ := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Keep", Cards: makeEntries(1, 3),
	})
	deck2, _ := svc.CreateDeck(context.Background(), pid, CreateDeckRequest{
		DeckName: "Delete", Cards: makeEntries(2, 3),
	})

	_ = svc.DeleteDeck(context.Background(), pid, deck2.DeckID)

	decks, _ := svc.GetDecks(context.Background(), pid)
	require.Len(t, decks, 1)
	assert.Equal(t, deck1.DeckID, decks[0].DeckID)
}

// ---------------------------------------------------------------------------
// RestrictionCopyCount tests
// ---------------------------------------------------------------------------

func TestRestrictionCopyCount(t *testing.T) {
	tests := []struct {
		restriction string
		want        int
	}{
		{"unlimited", 3},
		{"semi_limited", 2},
		{"limited", 1},
		{"", 3},
	}

	for _, tt := range tests {
		t.Run(tt.restriction, func(t *testing.T) {
			assert.Equal(t, tt.want, restrictionCopyCount(tt.restriction))
		})
	}
}
