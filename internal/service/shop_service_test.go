package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type testShopEnv struct {
	svc        *ShopService
	shopRepo   *repository.MockShopRepository
	subRepo    *repository.MockSubscriptionRepository
	playerRepo *repository.MockPlayerRepository
	cardCache  *cache.CardCache
}

func newTestShopEnv() *testShopEnv {
	shopRepo := repository.NewMockShopRepository()
	playerRepo := repository.NewMockPlayerRepository()
	cc := cache.NewCardCache()

	// SHE: unlimited=2, limited=1, semi_limited=1, inactive=1
	cc.InjectForTest("SH-0001", &model.CardDefinition{CardID: "SH-0001", CardName: "SHE Compute", Faction: "SHE", CardType: "Compute", Restriction: "unlimited", IsActive: true})
	cc.InjectForTest("SH-0002", &model.CardDefinition{CardID: "SH-0002", CardName: "SHE RDB", Faction: "SHE", CardType: "Database", Restriction: "unlimited", IsActive: true})
	cc.InjectForTest("SH-0003", &model.CardDefinition{CardID: "SH-0003", CardName: "SHE Limited", Faction: "SHE", CardType: "Strategy", Restriction: "limited", IsActive: true})
	cc.InjectForTest("SH-0004", &model.CardDefinition{CardID: "SH-0004", CardName: "SHE SemiLimited", Faction: "SHE", CardType: "Strategy", Restriction: "semi_limited", IsActive: true})
	cc.InjectForTest("SH-0005", &model.CardDefinition{CardID: "SH-0005", CardName: "SHE Inactive", Faction: "SHE", CardType: "Compute", Restriction: "unlimited", IsActive: false})

	// Neutral: unlimited=1, limited=1
	cc.InjectForTest("NT-0001", &model.CardDefinition{CardID: "NT-0001", CardName: "Neutral Card 1", Faction: "Neutral", CardType: "Strategy", Restriction: "unlimited", IsActive: true})
	cc.InjectForTest("NT-0002", &model.CardDefinition{CardID: "NT-0002", CardName: "Neutral Card 2", Faction: "Neutral", CardType: "Incident", Restriction: "limited", IsActive: true})

	// Tenki
	cc.InjectForTest("TK-0001", &model.CardDefinition{CardID: "TK-0001", CardName: "Tenki VM", Faction: "Tenki", CardType: "Compute", Restriction: "unlimited", IsActive: true})

	factionRepo := repository.NewMockFactionRepository()
	verifier := &platform.MockReceiptVerifier{}

	subRepo := repository.NewMockSubscriptionRepository()
	svc := NewShopService(shopRepo, subRepo, playerRepo, factionRepo, &repository.MockTxRunner{}, cc, verifier, verifier)

	return &testShopEnv{svc: svc, shopRepo: shopRepo, subRepo: subRepo, playerRepo: playerRepo, cardCache: cc}
}

func createTestPlayer(env *testShopEnv, playerID string) {
	_ = env.playerRepo.Create(context.Background(), &model.Player{
		PlayerID:    playerID,
		FirebaseUID: "uid-" + playerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, &model.PlayerDailyBattle{PlayerID: playerID})
}

// ---------------------------------------------------------------------------
// SelectFaction tests
// ---------------------------------------------------------------------------

func TestSelectFaction_Success(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	count, err := env.svc.SelectFaction(context.Background(), "p1", "she")
	require.NoError(t, err)

	t.Run("returns correct card count", func(t *testing.T) {
		// SHE active cards: SH-0001(3) + SH-0002(3) + SH-0003(1) + SH-0004(2) = 4 entries, 9 copies
		// Neutral cards: NT-0001(3) + NT-0002(1) = 2 entries, 4 copies
		// Total entries = 6
		assert.Equal(t, 6, count)
	})

	t.Run("sets faction on player", func(t *testing.T) {
		p, _ := env.playerRepo.FindByID(context.Background(), "p1")
		require.NotNil(t, p.SelectedFaction)
		assert.Equal(t, "SHE", *p.SelectedFaction)
	})

	t.Run("inserts faction cards", func(t *testing.T) {
		cards := env.shopRepo.GetPlayerCardsForTest("p1")
		assert.Len(t, cards, 6)
	})
}

func TestSelectFaction_InvalidFactions(t *testing.T) {
	tests := []struct {
		name    string
		faction string
	}{
		{"InvalidFaction", "InvalidFaction"},
		{"NeutralIsInvalid", "neutral"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestShopEnv()
			createTestPlayer(env, "p1")

			_, err := env.svc.SelectFaction(context.Background(), "p1", tt.faction)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidFaction)
		})
	}
}

func TestSelectFaction_AlreadySelected(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	// First selection succeeds
	_, err := env.svc.SelectFaction(context.Background(), "p1", "she")
	require.NoError(t, err)

	// Second selection fails
	_, err = env.svc.SelectFaction(context.Background(), "p1", "tenki")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFactionAlreadySelected)
}

func TestSelectFaction_CaseInsensitive(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	_, err := env.svc.SelectFaction(context.Background(), "p1", "SHE")
	require.NoError(t, err)

	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	require.NotNil(t, p.SelectedFaction)
	assert.Equal(t, "SHE", *p.SelectedFaction)
}

// ---------------------------------------------------------------------------
// BuildFactionCards tests
// ---------------------------------------------------------------------------

func TestBuildFactionCards_Copies(t *testing.T) {
	env := newTestShopEnv()

	cards := env.svc.buildFactionCards("p1", "SHE")

	// Expected: SH-0001, SH-0002, SH-0003, SH-0004 (SH-0005 inactive → excluded)
	require.Len(t, cards, 4)

	counts := make(map[string]int)
	for _, c := range cards {
		counts[c.CardID] = c.Count
	}
	assert.Equal(t, 3, counts["SH-0001"], "SH-0001 (unlimited) — all cards get 3 copies at grant time")
	assert.Equal(t, 3, counts["SH-0003"], "SH-0003 (limited) — restriction applies at deck build, not grant")
	assert.Equal(t, 3, counts["SH-0004"], "SH-0004 (semi_limited) — restriction applies at deck build, not grant")
	assert.Equal(t, 0, counts["SH-0005"], "SH-0005 (inactive)")
}

func TestBuildFactionCards_Neutral(t *testing.T) {
	env := newTestShopEnv()

	cards := env.svc.buildFactionCards("p1", "Neutral")
	require.Len(t, cards, 2)

	total := 0
	for _, c := range cards {
		total += c.Count
	}
	assert.Equal(t, 6, total)
}

func TestBuildFactionCards_UnknownFaction(t *testing.T) {
	env := newTestShopEnv()

	cards := env.svc.buildFactionCards("p1", "Unknown")
	assert.Len(t, cards, 0)
}

// ---------------------------------------------------------------------------
// Purchase tests
// ---------------------------------------------------------------------------

func TestPurchase_FactionSet_Success(t *testing.T) {
	env := newTestShopEnv()

	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-123", ProductID: "faction_tenki"}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "faction_tenki", "ios", "receipt-token-1")
	require.NoError(t, err)

	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	assert.Len(t, cards, 1)
	if len(cards) > 0 {
		assert.Equal(t, 3, cards[0].Count)
	}
}

func TestPurchase_Idempotent(t *testing.T) {
	env := newTestShopEnv()

	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-123"}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	})

	ctx := context.Background()
	_ = env.svc.Purchase(ctx, "p1", "faction_tenki", "ios", "receipt-token-1")

	// Second purchase with same token — idempotent
	err := env.svc.Purchase(ctx, "p1", "faction_tenki", "ios", "receipt-token-1")
	require.NoError(t, err)

	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	assert.Len(t, cards, 1)
}

func TestPurchase_ReceiptFailed(t *testing.T) {
	env := newTestShopEnv()

	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: false}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "faction_tenki", "ios", "bad-receipt")
	assert.ErrorIs(t, err, ErrReceiptVerificationFailed)

	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	assert.Len(t, cards, 0)
}

func TestPurchase_CosmeticItem(t *testing.T) {
	env := newTestShopEnv()

	env.svc.googleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-456"}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "playmat_01",
		Name:      "プレイマット: サイバー",
		Type:      model.ProductTypeCosmetic,
		Price:     320,
		Content:   json.RawMessage(`{"item_type":"playmat","item_no":1}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "playmat_01", "android", "cosmetic-receipt")
	require.NoError(t, err)
}

func TestPurchase_InactiveProduct(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "old_product",
		Name:      "旧商品",
		Type:      model.ProductTypeFactionSet,
		Price:     100,
		Content:   json.RawMessage(`{"faction":"SHE"}`),
		IsActive:  false,
	})

	err := env.svc.Purchase(context.Background(), "p1", "old_product", "ios", "receipt-1")
	assert.ErrorIs(t, err, ErrProductNotActive)
}

func TestPurchase_UnsupportedPlatform(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_she",
		Name:      "SHEカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SHE"}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "faction_she", "windows", "receipt-1")
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

// ---------------------------------------------------------------------------
// Subscribe tests
// ---------------------------------------------------------------------------

func TestSubscribe_Success(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifySubscriptionFn: func(ctx context.Context, token string) (*platform.SubscriptionInfo, error) {
			return &platform.SubscriptionInfo{
				IsValid:   true,
				ProductID: "premium_monthly",
				ExpiresAt: expiresAt,
			}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "premium_monthly",
		Name:      "プレミアム月額",
		Type:      model.ProductTypeSubscription,
		Price:     480,
		Content:   json.RawMessage(`{}`),
		IsActive:  true,
	})

	result, err := env.svc.Subscribe(context.Background(), "p1", "premium_monthly", "ios", "sub-token-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	t.Run("updates player to premium", func(t *testing.T) {
		p, _ := env.playerRepo.FindByID(context.Background(), "p1")
		assert.True(t, p.IsPremium)
		assert.NotNil(t, p.PremiumExpiresAt)
	})

	t.Run("creates active subscription record", func(t *testing.T) {
		sub, _ := env.subRepo.GetActiveSubscription(context.Background(), "p1")
		require.NotNil(t, sub)
		assert.Equal(t, model.SubscriptionStatusActive, sub.Status)
	})
}

func TestSubscribe_Errors(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(env *testShopEnv)
		productID string
		platform  string
		token     string
		wantErr   error
	}{
		{
			name: "NotSubscriptionProduct",
			setup: func(env *testShopEnv) {
				env.shopRepo.AddProduct(&model.Product{
					ProductID: "faction_she",
					Name:      "SHEカードセット",
					Type:      model.ProductTypeFactionSet,
					Price:     980,
					Content:   json.RawMessage(`{"faction":"SHE"}`),
					IsActive:  true,
				})
			},
			productID: "faction_she",
			platform:  "ios",
			token:     "sub-token-1",
			wantErr:   ErrProductNotSubscription,
		},
		{
			name: "VerificationFailed",
			setup: func(env *testShopEnv) {
				env.svc.appleVerifier = &platform.MockReceiptVerifier{
					VerifySubscriptionFn: func(ctx context.Context, token string) (*platform.SubscriptionInfo, error) {
						return &platform.SubscriptionInfo{IsValid: false}, nil
					},
				}
				env.shopRepo.AddProduct(&model.Product{
					ProductID: "premium_monthly",
					Name:      "プレミアム月額",
					Type:      model.ProductTypeSubscription,
					Price:     480,
					Content:   json.RawMessage(`{}`),
					IsActive:  true,
				})
			},
			productID: "premium_monthly",
			platform:  "ios",
			token:     "bad-sub-token",
			wantErr:   ErrSubVerificationFailed,
		},
		{
			name: "UnsupportedPlatform",
			setup: func(env *testShopEnv) {
				env.shopRepo.AddProduct(&model.Product{
					ProductID: "premium_monthly",
					Name:      "プレミアム月額",
					Type:      model.ProductTypeSubscription,
					Price:     480,
					Content:   json.RawMessage(`{}`),
					IsActive:  true,
				})
			},
			productID: "premium_monthly",
			platform:  "windows",
			token:     "sub-token-1",
			wantErr:   ErrUnsupportedPlatform,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestShopEnv()
			tt.setup(env)

			_, err := env.svc.Subscribe(context.Background(), "p1", tt.productID, tt.platform, tt.token)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestSubscribe_AndroidPlatform(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	env.svc.googleVerifier = &platform.MockReceiptVerifier{
		VerifySubscriptionFn: func(ctx context.Context, token string) (*platform.SubscriptionInfo, error) {
			return &platform.SubscriptionInfo{
				IsValid:   true,
				ProductID: "premium_monthly",
				ExpiresAt: expiresAt,
			}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "premium_monthly",
		Name:      "プレミアム月額",
		Type:      model.ProductTypeSubscription,
		Price:     480,
		Content:   json.RawMessage(`{}`),
		IsActive:  true,
	})

	result, err := env.svc.Subscribe(context.Background(), "p1", "premium_monthly", "android", "sub-token-android")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// ---------------------------------------------------------------------------
// GetProducts tests
// ---------------------------------------------------------------------------

func TestGetProducts_WithOwnership(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_she",
		Name:      "SHEカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SHE"}`),
		IsActive:  true,
	})
	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	})

	// Simulate player owning SD faction via purchase
	_ = env.shopRepo.CreatePurchaseWithCards(context.Background(), &model.OneTimePurchase{
		PlayerID:      "p1",
		ProductID:     "faction_she",
		Platform:      "ios",
		PurchaseToken: "test-token-sd",
		PurchasedAt:   time.Now(),
	}, nil)

	products, err := env.svc.GetProducts(context.Background(), "p1")
	require.NoError(t, err)
	require.Len(t, products, 2)

	// Build map for easy lookup
	byID := map[string]ProductWithOwnership{}
	for _, p := range products {
		byID[p.ProductID] = p
	}
	assert.True(t, byID["faction_she"].IsOwned)
	assert.False(t, byID["faction_tenki"].IsOwned)
}

func TestGetProducts_SubscriptionOwnership(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "premium_monthly",
		Name:      "プレミアム月額",
		Type:      model.ProductTypeSubscription,
		Price:     480,
		Content:   json.RawMessage(`{}`),
		IsActive:  true,
	})

	// Create active subscription
	now := time.Now()
	_ = env.subRepo.CreateSubscription(context.Background(), &model.Subscription{
		PlayerID:           "p1",
		ProductID:          "premium_monthly",
		Platform:           "ios",
		PurchaseToken:      "sub-token",
		Status:             model.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	})

	products, err := env.svc.GetProducts(context.Background(), "p1")
	require.NoError(t, err)
	require.Len(t, products, 1)
	assert.True(t, products[0].IsOwned)
}


// ---------------------------------------------------------------------------
// NormalizeFaction tests
// ---------------------------------------------------------------------------

func TestNormalizeFaction(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"she", "SHE", true},
		{"SHE", "SHE", true},
		{"tenki", "Tenki", true},
		{"TENKI", "Tenki", true},
		{"sugar", "Sugar", true},
		{"tuners", "Tuners", true},
		{"neutral", "Neutral", true},
		{"invalid", "invalid", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := normalizeFaction(tt.input)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

// ---------------------------------------------------------------------------
// getVerifier tests
// ---------------------------------------------------------------------------

func TestGetVerifier(t *testing.T) {
	env := newTestShopEnv()

	tests := []struct {
		platform string
		wantNil  bool
	}{
		{"ios", false},
		{"android", false},
		{"windows", true},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			v := env.svc.getVerifier(tt.platform)
			if tt.wantNil {
				assert.Nil(t, v)
			} else {
				assert.NotNil(t, v)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Additional tests
// ---------------------------------------------------------------------------

func TestSelectFaction_PlayerNotFound(t *testing.T) {
	env := newTestShopEnv()
	// Do NOT create the player — "nonexistent" has no record in playerRepo.

	_, err := env.svc.SelectFaction(context.Background(), "nonexistent", "she")
	require.Error(t, err)
}

func TestPurchase_VerifierReturnsError(t *testing.T) {
	env := newTestShopEnv()

	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return nil, fmt.Errorf("network timeout")
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_she",
		Name:      "SHEカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SHE"}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "faction_she", "ios", "receipt-err")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "verify receipt:"))
}

func TestPurchase_SubscriptionTypeViaPurchase(t *testing.T) {
	env := newTestShopEnv()

	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-sub-via-purchase"}, nil
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "premium_monthly",
		Name:      "プレミアム月額",
		Type:      model.ProductTypeSubscription,
		Price:     480,
		Content:   json.RawMessage(`{}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "premium_monthly", "ios", "receipt-sub")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unsupported product type"))
}

func TestSelectFaction_CardCopiesVerified(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	_, err := env.svc.SelectFaction(context.Background(), "p1", "she")
	require.NoError(t, err)

	cards := env.shopRepo.GetPlayerCardsForTest("p1")

	// Build a map of cardID -> count for easy lookup.
	counts := make(map[string]int)
	for _, c := range cards {
		counts[c.CardID] = c.Count
	}

	// All cards get 3 copies regardless of restriction.
	// SHE cards: SH-0001, SH-0002, SH-0003, SH-0004; Neutral cards: NT-0001, NT-0002
	expected := map[string]int{
		"SH-0001": 3,
		"SH-0002": 3,
		"SH-0003": 3,
		"SH-0004": 3,
		"NT-0001": 3,
		"NT-0002": 3,
	}

	for cardID, wantCount := range expected {
		gotCount, ok := counts[cardID]
		require.True(t, ok, "card %s not found in player cards", cardID)
		assert.Equal(t, wantCount, gotCount, "card %s", cardID)
	}
}
