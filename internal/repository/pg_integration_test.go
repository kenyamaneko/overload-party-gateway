package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

func setupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := os.Getenv("TEST_DB_URL")
	if connStr == "" {
		t.Skip("TEST_DB_URL not set; run: docker compose -f ../overload-party-common/db/docker-compose.test.yml up -d")
	}

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Truncate all tables for test isolation
	tables := []string{
		"deck_cards", "decks", "player_cards", "player_items",
		"one_time_purchases", "subscriptions", "user_settings",
		"player_daily_battle", "players",
		"card_definitions", "products", "game_config", "news_articles",
	}
	for _, table := range tables {
		_, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE %s CASCADE", table))
		require.NoError(t, err)
	}

	return pool
}

func newID() string { return uuid.New().String() }

// ---------------------------------------------------------------------------
// PgPlayerRepository
// ---------------------------------------------------------------------------

func TestPgPlayer_CreateAndFindByID(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	pid := newID()

	player := &model.Player{
		PlayerID:    pid,
		FirebaseUID: "firebase-001",
		Username:    "Alice",
		Level:       1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	daily := &model.PlayerDailyBattle{
		PlayerID:         pid,
		DailyBattleCount: 0,
		LastResetDate:    civil.DateOf(now),
	}

	err := repo.Create(ctx, player, daily)
	require.NoError(t, err)

	found, err := repo.FindByID(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Alice", found.Username)
	assert.Equal(t, "firebase-001", found.FirebaseUID)
}

func TestPgPlayer_FindByID_NotFound(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)

	found, err := repo.FindByID(context.Background(), newID())
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestPgPlayer_FindByFirebaseUID(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	pid := newID()

	_ = repo.Create(ctx, &model.Player{
		PlayerID: pid, FirebaseUID: "fb-002", Username: "Bob",
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, LastResetDate: civil.DateOf(now),
	})

	found, err := repo.FindByFirebaseUID(ctx, "fb-002")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, pid, found.PlayerID)

	notFound, err := repo.FindByFirebaseUID(ctx, "no-such-uid")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestPgPlayer_GetDailyBattle(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	today := civil.DateOf(now)
	pid := newID()

	_ = repo.Create(ctx, &model.Player{
		PlayerID: pid, FirebaseUID: newID(), Username: "Carol",
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, DailyBattleCount: 5, LastResetDate: today,
	})

	db, err := repo.GetDailyBattle(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, db)
	assert.Equal(t, int64(5), db.DailyBattleCount)
	assert.Equal(t, today, db.LastResetDate)

	nilDB, err := repo.GetDailyBattle(ctx, newID())
	require.NoError(t, err)
	assert.Nil(t, nilDB)
}

func TestPgPlayer_IncrementDailyBattle(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	today := civil.DateOf(now)
	pid := newID()

	_ = repo.Create(ctx, &model.Player{
		PlayerID: pid, FirebaseUID: newID(), Username: "Dan",
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, DailyBattleCount: 0, LastResetDate: today,
	})

	count, err := repo.IncrementDailyBattle(ctx, pid, today)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	count, err = repo.IncrementDailyBattle(ctx, pid, today)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// New day resets
	tomorrow := today.AddDays(1)
	count, err = repo.IncrementDailyBattle(ctx, pid, tomorrow)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestPgPlayer_UpdateUsername(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	pid := newID()

	_ = repo.Create(ctx, &model.Player{
		PlayerID: pid, FirebaseUID: newID(), Username: "Eve",
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, LastResetDate: civil.DateOf(now),
	})

	updated, err := repo.UpdateUsername(ctx, pid, "Eva")
	require.NoError(t, err)
	assert.Equal(t, "Eva", updated.Username)
}

func TestPgPlayer_UpdatePremium(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	pid := newID()

	_ = repo.Create(ctx, &model.Player{
		PlayerID: pid, FirebaseUID: newID(), Username: "Frank",
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, LastResetDate: civil.DateOf(now),
	})

	expires := now.Add(30 * 24 * time.Hour)
	err := repo.UpdatePremium(ctx, pid, true, &expires)
	require.NoError(t, err)

	found, _ := repo.FindByID(ctx, pid)
	assert.True(t, found.IsPremium)
}

func TestPgPlayer_UpdateFaction(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgPlayerRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	pid := newID()

	_ = repo.Create(ctx, &model.Player{
		PlayerID: pid, FirebaseUID: newID(), Username: "Grace",
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, LastResetDate: civil.DateOf(now),
	})

	err := repo.UpdateFaction(ctx, pid, "CloudForge")
	require.NoError(t, err)

	found, _ := repo.FindByID(ctx, pid)
	require.NotNil(t, found.SelectedFaction)
	assert.Equal(t, "CloudForge", *found.SelectedFaction)
}

// ---------------------------------------------------------------------------
// PgDeckRepository
// ---------------------------------------------------------------------------

func createTestPlayer(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	pid := newID()
	playerRepo := NewPgPlayerRepository(pool)
	now := time.Now().Truncate(time.Second)
	err := playerRepo.Create(context.Background(), &model.Player{
		PlayerID: pid, FirebaseUID: newID(), Username: name,
		CreatedAt: now, UpdatedAt: now,
	}, &model.PlayerDailyBattle{
		PlayerID: pid, LastResetDate: civil.DateOf(now),
	})
	require.NoError(t, err)
	return pid
}

func seedCardDefinitions(t *testing.T, pool *pgxpool.Pool, cardIDs ...string) {
	t.Helper()
	ctx := context.Background()
	for _, id := range cardIDs {
		_, err := pool.Exec(ctx,
			`INSERT INTO card_definitions (card_id, card_name, resource_label, faction, card_type, resizable, elastic, stats, restriction, is_active, created_at, updated_at)
			 VALUES ($1, $2, 'res', 'Neutral', 'resource', false, false, '{}', 'unlimited', true, NOW(), NOW())
			 ON CONFLICT (card_id) DO NOTHING`,
			id, fmt.Sprintf("Card-%s", id))
		require.NoError(t, err)
	}
}

func seedPlayerCards(t *testing.T, pool *pgxpool.Pool, playerID string, cards []model.PlayerCard) {
	t.Helper()
	ctx := context.Background()
	for _, c := range cards {
		_, err := pool.Exec(ctx,
			`INSERT INTO player_cards (player_id, card_id, art_no, count)
			 VALUES ($1,$2,$3,$4)
			 ON CONFLICT (player_id, card_id, art_no)
			 DO UPDATE SET count = player_cards.count + EXCLUDED.count`,
			playerID, c.CardID, c.ArtNo, c.Count)
		require.NoError(t, err)
	}
}

func TestPgDeck_CreateAndFindByPlayerID(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "DeckUser")
	repo := NewPgDeckRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	deck := &model.Deck{
		PlayerID: pid, DeckName: "My Deck",
		CreatedAt: now, UpdatedAt: now,
	}
	cards := []model.DeckCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 3},
		{PlayerID: pid, CardID: "C-002", ArtNo: 1, Count: 2},
	}

	err := repo.Create(ctx, deck, cards)
	require.NoError(t, err)
	assert.NotZero(t, deck.DeckID)

	decks, err := repo.FindByPlayerID(ctx, pid)
	require.NoError(t, err)
	assert.Len(t, decks, 1)
	assert.Equal(t, "My Deck", decks[0].DeckName)
}

func TestPgDeck_FindByID(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "DeckUser2")
	repo := NewPgDeckRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	deck := &model.Deck{
		PlayerID: pid, DeckName: "Test Deck",
		CreatedAt: now, UpdatedAt: now,
	}
	_ = repo.Create(ctx, deck, nil)

	found, err := repo.FindByID(ctx, pid, deck.DeckID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Test Deck", found.DeckName)

	notFound, err := repo.FindByID(ctx, pid, 99999)
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestPgDeck_GetDeckCards(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "DeckUser3")
	repo := NewPgDeckRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	deck := &model.Deck{
		PlayerID: pid, DeckName: "Card Deck",
		CreatedAt: now, UpdatedAt: now,
	}
	cards := []model.DeckCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 3},
		{PlayerID: pid, CardID: "C-002", ArtNo: 1, Count: 2},
	}
	_ = repo.Create(ctx, deck, cards)

	got, err := repo.GetDeckCards(ctx, pid, deck.DeckID)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestPgDeck_GetPlayerCards(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "DeckUser4")
	seedCardDefinitions(t, pool, "C-001", "C-002")
	seedPlayerCards(t, pool, pid, []model.PlayerCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 5},
		{PlayerID: pid, CardID: "C-002", ArtNo: 1, Count: 3},
	})
	repo := NewPgPlayerCardRepository(pool)

	cards, err := repo.GetPlayerCards(context.Background(), pid)
	require.NoError(t, err)
	assert.Len(t, cards, 2)
	assert.Equal(t, "C-001", cards[0].CardID)
}

func TestPgDeck_Update(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "DeckUser5")
	repo := NewPgDeckRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	deck := &model.Deck{
		PlayerID: pid, DeckName: "Original",
		CreatedAt: now, UpdatedAt: now,
	}
	oldCards := []model.DeckCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 3},
	}
	_ = repo.Create(ctx, deck, oldCards)

	deck.DeckName = "Updated"
	newCards := []model.DeckCard{
		{PlayerID: pid, DeckID: deck.DeckID, CardID: "C-002", ArtNo: 1, Count: 2},
		{PlayerID: pid, DeckID: deck.DeckID, CardID: "C-003", ArtNo: 1, Count: 1},
	}
	err := repo.Update(ctx, deck, newCards)
	require.NoError(t, err)

	found, _ := repo.FindByID(ctx, pid, deck.DeckID)
	assert.Equal(t, "Updated", found.DeckName)

	gotCards, _ := repo.GetDeckCards(ctx, pid, deck.DeckID)
	assert.Len(t, gotCards, 2)
}

func TestPgDeck_Delete(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "DeckUser6")
	repo := NewPgDeckRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	deck := &model.Deck{
		PlayerID: pid, DeckName: "ToDelete",
		CreatedAt: now, UpdatedAt: now,
	}
	_ = repo.Create(ctx, deck, nil)

	err := repo.Delete(ctx, pid, deck.DeckID)
	require.NoError(t, err)

	found, err := repo.FindByID(ctx, pid, deck.DeckID)
	require.NoError(t, err)
	assert.Nil(t, found)
}

// ---------------------------------------------------------------------------
// PgCardRepository
// ---------------------------------------------------------------------------

func TestPgCard_FindAll(t *testing.T) {
	pool := setupPool(t)
	seedCardDefinitions(t, pool, "C-001", "C-002", "C-003")
	repo := NewPgCardRepository(pool)

	cards, err := repo.FindAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, cards, 3)
	assert.Equal(t, "C-001", cards[0].CardID)
}

func TestPgCard_FindByCardID(t *testing.T) {
	pool := setupPool(t)
	seedCardDefinitions(t, pool, "C-010")
	repo := NewPgCardRepository(pool)

	card, err := repo.FindByCardID(context.Background(), "C-010")
	require.NoError(t, err)
	require.NotNil(t, card)
	assert.Equal(t, "C-010", card.CardID)

	notFound, err := repo.FindByCardID(context.Background(), "C-999")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

// ---------------------------------------------------------------------------
// PgUserSettingsRepository
// ---------------------------------------------------------------------------

func TestPgUserSettings_UpsertAndGet(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "SettingsUser")
	repo := NewPgUserSettingsRepository(pool)
	ctx := context.Background()

	// Get before insert → nil
	got, err := repo.Get(ctx, pid)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Upsert
	settings := &model.UserSettings{
		PlayerID:    pid,
		Language:    "ja",
		BgmVolume:   80,
		SeVolume:    60,
		PushEnabled: true,
	}
	err = repo.Upsert(ctx, settings)
	require.NoError(t, err)

	got, err = repo.Get(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "ja", got.Language)
	assert.Equal(t, int64(80), got.BgmVolume)

	// Update via upsert
	settings.Language = "en"
	settings.BgmVolume = 50
	err = repo.Upsert(ctx, settings)
	require.NoError(t, err)

	got, err = repo.Get(ctx, pid)
	require.NoError(t, err)
	assert.Equal(t, "en", got.Language)
	assert.Equal(t, int64(50), got.BgmVolume)
}

// ---------------------------------------------------------------------------
// PgGameConfigRepository
// ---------------------------------------------------------------------------

func TestPgGameConfig_GetInt64(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgGameConfigRepository(pool)
	ctx := context.Background()

	// Fallback when key doesn't exist
	val, err := repo.GetInt64(ctx, "nonexistent_key", 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)

	// Insert a config value
	_, err = pool.Exec(ctx, `INSERT INTO game_config (key, value) VALUES ($1, $2)`,
		"test_key", json.RawMessage(`100`))
	require.NoError(t, err)

	val, err = repo.GetInt64(ctx, "test_key", 0)
	require.NoError(t, err)
	assert.Equal(t, int64(100), val)
}

// ---------------------------------------------------------------------------
// PgShopRepository
// ---------------------------------------------------------------------------

func seedProducts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO products (product_id, name, type, price, content, is_active) VALUES
		 ('prod-1', 'Starter Pack', 'faction_set', 500, '{"faction":"CloudForge"}', true),
		 ('prod-2', 'Icon Pack', 'cosmetic', 300, '{"item_type":"icon","item_no":1}', true),
		 ('prod-inactive', 'Old Pack', 'faction_set', 100, '{}', false)`)
	require.NoError(t, err)
}

func TestPgShop_GetActiveProducts(t *testing.T) {
	pool := setupPool(t)
	seedProducts(t, pool)
	repo := NewPgShopRepository(pool)

	products, err := repo.GetActiveProducts(context.Background())
	require.NoError(t, err)
	assert.Len(t, products, 2)
}

func TestPgShop_GetProductByID(t *testing.T) {
	pool := setupPool(t)
	seedProducts(t, pool)
	repo := NewPgShopRepository(pool)

	p, err := repo.GetProductByID(context.Background(), "prod-1")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "Starter Pack", p.Name)

	notFound, err := repo.GetProductByID(context.Background(), "no-such")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestPgShop_CreatePurchaseWithCards(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "ShopUser")
	seedProducts(t, pool)
	seedCardDefinitions(t, pool, "C-001", "C-002")
	repo := NewPgShopRepository(pool)
	ctx := context.Background()

	purchase := &model.OneTimePurchase{
		PlayerID:      pid,
		ProductID:     "prod-1",
		Platform:      model.PlatformIOS,
		PurchaseToken: "token-abc",
		PurchasedAt:   time.Now(),
	}
	cards := []*model.PlayerCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 3},
		{PlayerID: pid, CardID: "C-002", ArtNo: 1, Count: 2},
	}

	err := repo.CreatePurchaseWithCards(ctx, purchase, cards)
	require.NoError(t, err)
	assert.NotZero(t, purchase.PurchaseID)

	// Idempotent: same token should not fail
	err = repo.CreatePurchaseWithCards(ctx, purchase, cards)
	require.NoError(t, err)

	// Verify player cards were inserted
	pcRepo := NewPgPlayerCardRepository(pool)
	pc, _ := pcRepo.GetPlayerCards(ctx, pid)
	assert.Len(t, pc, 2)
}

func TestPgShop_FindPurchaseByToken(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "ShopUser2")
	seedProducts(t, pool)
	repo := NewPgShopRepository(pool)
	ctx := context.Background()

	purchase := &model.OneTimePurchase{
		PlayerID:      pid,
		ProductID:     "prod-1",
		Platform:      model.PlatformAndroid,
		PurchaseToken: "token-find",
		PurchasedAt:   time.Now(),
	}
	_ = repo.CreatePurchaseWithCards(ctx, purchase, nil)

	found, err := repo.FindPurchaseByToken(ctx, pid, "token-find")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "prod-1", found.ProductID)

	notFound, err := repo.FindPurchaseByToken(ctx, pid, "no-such-token")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestPgShop_Subscription_CRUD(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "SubUser")
	seedProducts(t, pool)
	repo := NewPgShopRepository(pool)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	sub := &model.Subscription{
		PlayerID:           pid,
		ProductID:          "prod-1",
		Platform:           model.PlatformIOS,
		PurchaseToken:      "sub-token-1",
		Status:             model.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	err := repo.CreateSubscription(ctx, sub)
	require.NoError(t, err)
	assert.NotZero(t, sub.SubscriptionID)

	// GetActiveSubscription
	active, err := repo.GetActiveSubscription(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, model.SubscriptionStatusActive, active.Status)

	// FindSubscriptionByToken
	found, err := repo.FindSubscriptionByToken(ctx, "sub-token-1")
	require.NoError(t, err)
	require.NotNil(t, found)

	// UpdateSubscription
	sub.Status = model.SubscriptionStatusExpired
	sub.UpdatedAt = time.Now()
	err = repo.UpdateSubscription(ctx, sub)
	require.NoError(t, err)

	// No longer active
	active, err = repo.GetActiveSubscription(ctx, pid)
	require.NoError(t, err)
	assert.Nil(t, active)
}

func TestPgShop_InsertPlayerCards(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "CardUser")
	seedCardDefinitions(t, pool, "C-001")
	repo := NewPgShopRepository(pool)
	ctx := context.Background()

	cards := []*model.PlayerCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 2},
	}
	err := repo.InsertPlayerCards(ctx, cards)
	require.NoError(t, err)

	// Upsert: add more
	cards2 := []*model.PlayerCard{
		{PlayerID: pid, CardID: "C-001", ArtNo: 1, Count: 3},
	}
	err = repo.InsertPlayerCards(ctx, cards2)
	require.NoError(t, err)

	pcRepo := NewPgPlayerCardRepository(pool)
	pc, _ := pcRepo.GetPlayerCards(ctx, pid)
	require.Len(t, pc, 1)
	assert.Equal(t, 5, pc[0].Count) // 2 + 3
}

func TestPgShop_GetPlayerOwnedFactions(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "FactionUser")
	ctx := context.Background()

	// Insert card definitions with different factions
	_, _ = pool.Exec(ctx,
		`INSERT INTO card_definitions (card_id, card_name, resource_label, faction, card_type, resizable, elastic, stats, restriction, is_active, created_at, updated_at) VALUES
		 ('CF-001', 'CF Card', 'res', 'CloudForge', 'resource', false, false, '{}', 'unlimited', true, NOW(), NOW()),
		 ('NX-001', 'NX Card', 'res', 'NetX', 'resource', false, false, '{}', 'unlimited', true, NOW(), NOW()),
		 ('NT-001', 'Neutral Card', 'res', 'Neutral', 'resource', false, false, '{}', 'unlimited', true, NOW(), NOW())
		 ON CONFLICT (card_id) DO NOTHING`)

	seedPlayerCards(t, pool, pid, []model.PlayerCard{
		{PlayerID: pid, CardID: "CF-001", ArtNo: 1, Count: 1},
		{PlayerID: pid, CardID: "NX-001", ArtNo: 1, Count: 1},
		{PlayerID: pid, CardID: "NT-001", ArtNo: 1, Count: 1},
	})

	repo := NewPgShopRepository(pool)
	factions, err := repo.GetPlayerOwnedFactions(ctx, pid)
	require.NoError(t, err)
	assert.Len(t, factions, 2) // CloudForge and NetX, not Neutral
}

func TestPgShop_CreatePurchaseWithItem(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "ItemUser")
	seedProducts(t, pool)
	repo := NewPgShopRepository(pool)
	ctx := context.Background()

	purchase := &model.OneTimePurchase{
		PlayerID:      pid,
		ProductID:     "prod-2",
		Platform:      model.PlatformIOS,
		PurchaseToken: "token-item-1",
		PurchasedAt:   time.Now(),
	}
	item := &model.PlayerItem{
		PlayerID:   pid,
		ItemType:   "icon",
		ItemNo:     1,
		AcquiredAt: time.Now(),
	}

	err := repo.CreatePurchaseWithItem(ctx, purchase, item)
	require.NoError(t, err)
	assert.NotZero(t, purchase.PurchaseID)

	// Idempotent
	err = repo.CreatePurchaseWithItem(ctx, purchase, item)
	require.NoError(t, err)
}

func TestPgShop_InsertPlayerItems(t *testing.T) {
	pool := setupPool(t)
	pid := createTestPlayer(t, pool, "ItemUser2")
	repo := NewPgShopRepository(pool)
	ctx := context.Background()

	items := []*model.PlayerItem{
		{PlayerID: pid, ItemType: "icon", ItemNo: 1, AcquiredAt: time.Now()},
		{PlayerID: pid, ItemType: "sleeve", ItemNo: 2, AcquiredAt: time.Now()},
	}
	err := repo.InsertPlayerItems(ctx, items)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// PgNewsRepository
// ---------------------------------------------------------------------------

func TestPgNews_List(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgNewsRepository(pool)
	ctx := context.Background()
	now := time.Now()

	// Insert articles
	for i := 0; i < 5; i++ {
		summary := fmt.Sprintf("Summary %d", i)
		_, err := pool.Exec(ctx,
			`INSERT INTO news_articles (article_id, source, source_url, title, summary, tags, published_at, fetched_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			fmt.Sprintf("article-%d", i),
			"aws",
			fmt.Sprintf("https://example.com/article-%d", i),
			fmt.Sprintf("Title %d", i),
			&summary,
			[]string{"tag1"},
			now.Add(time.Duration(-i)*time.Hour),
			now,
		)
		require.NoError(t, err)
	}

	articles, err := repo.List(ctx, 3, 0)
	require.NoError(t, err)
	assert.Len(t, articles, 3)

	// Offset
	articles2, err := repo.List(ctx, 10, 3)
	require.NoError(t, err)
	assert.Len(t, articles2, 2)

	// Empty
	empty, err := repo.List(ctx, 10, 10)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
