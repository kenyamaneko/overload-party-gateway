package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type testShopEnv struct {
	svc        *ShopService
	shopRepo   *repository.MockShopRepository
	playerRepo *repository.MockPlayerRepository
	cardCache  *cache.CardCache
}

func newTestShopEnv() *testShopEnv {
	shopRepo := repository.NewMockShopRepository()
	playerRepo := repository.NewMockPlayerRepository()
	cc := cache.NewCardCache()

	// SD: unlimited=2, limited=1, semi_limited=1, inactive=1
	cc.InjectForTest(1, &model.CardDefinition{CardNo: 1, CardName: "SD Compute", Faction: "SD", CardType: "Compute", Restriction: "unlimited", IsActive: true})
	cc.InjectForTest(2, &model.CardDefinition{CardNo: 2, CardName: "SD RDB", Faction: "SD", CardType: "Database", Restriction: "unlimited", IsActive: true})
	cc.InjectForTest(3, &model.CardDefinition{CardNo: 3, CardName: "SD Limited", Faction: "SD", CardType: "Strategy", Restriction: "limited", IsActive: true})
	cc.InjectForTest(4, &model.CardDefinition{CardNo: 4, CardName: "SD SemiLimited", Faction: "SD", CardType: "Strategy", Restriction: "semi_limited", IsActive: true})
	cc.InjectForTest(5, &model.CardDefinition{CardNo: 5, CardName: "SD Inactive", Faction: "SD", CardType: "Compute", Restriction: "unlimited", IsActive: false})

	// Neutral: unlimited=1, limited=1
	cc.InjectForTest(100, &model.CardDefinition{CardNo: 100, CardName: "Neutral Card 1", Faction: "Neutral", CardType: "Strategy", Restriction: "unlimited", IsActive: true})
	cc.InjectForTest(101, &model.CardDefinition{CardNo: 101, CardName: "Neutral Card 2", Faction: "Neutral", CardType: "Incident", Restriction: "limited", IsActive: true})

	// Tenki
	cc.InjectForTest(200, &model.CardDefinition{CardNo: 200, CardName: "Tenki VM", Faction: "Tenki", CardType: "Compute", Restriction: "unlimited", IsActive: true})

	verifier := &platform.MockReceiptVerifier{}

	svc := NewShopService(shopRepo, playerRepo, cc, verifier, verifier)

	return &testShopEnv{svc: svc, shopRepo: shopRepo, playerRepo: playerRepo, cardCache: cc}
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

	count, err := env.svc.SelectFaction(context.Background(), "p1", "sd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SD active cards: 1(3) + 2(3) + 3(1) + 4(2) = 4 entries, 9 copies
	// Neutral cards: 100(3) + 101(1) = 2 entries, 4 copies
	// Total entries = 6
	if count != 6 {
		t.Errorf("expected 6 card entries, got %d", count)
	}

	// Verify faction was set on player
	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	if p.SelectedFaction == nil || *p.SelectedFaction != "SD" {
		t.Errorf("expected SelectedFaction=SD, got %v", p.SelectedFaction)
	}

	// Verify cards were inserted
	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 6 {
		t.Errorf("expected 6 card entries in shop repo, got %d", len(cards))
	}
}

func TestSelectFaction_InvalidFaction(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	_, err := env.svc.SelectFaction(context.Background(), "p1", "InvalidFaction")
	if err == nil {
		t.Fatal("expected error for invalid faction")
	}
	if !errors.Is(err, ErrInvalidFaction) {
		t.Errorf("expected ErrInvalidFaction, got: %v", err)
	}
}

func TestSelectFaction_NeutralIsInvalid(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	_, err := env.svc.SelectFaction(context.Background(), "p1", "neutral")
	if err == nil {
		t.Fatal("expected error for neutral faction selection")
	}
	if !errors.Is(err, ErrInvalidFaction) {
		t.Errorf("expected ErrInvalidFaction, got: %v", err)
	}
}

func TestSelectFaction_AlreadySelected(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	// First selection succeeds
	_, err := env.svc.SelectFaction(context.Background(), "p1", "sd")
	if err != nil {
		t.Fatalf("first selection failed: %v", err)
	}

	// Second selection fails
	_, err = env.svc.SelectFaction(context.Background(), "p1", "tenki")
	if err == nil {
		t.Fatal("expected error for already selected faction")
	}
	if !errors.Is(err, ErrFactionAlreadySelected) {
		t.Errorf("expected ErrFactionAlreadySelected, got: %v", err)
	}
}

func TestSelectFaction_CaseInsensitive(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	_, err := env.svc.SelectFaction(context.Background(), "p1", "SD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	if p.SelectedFaction == nil || *p.SelectedFaction != "SD" {
		t.Errorf("expected SelectedFaction=SD, got %v", p.SelectedFaction)
	}
}

// ---------------------------------------------------------------------------
// BuildFactionCards tests
// ---------------------------------------------------------------------------

func TestBuildFactionCards_Copies(t *testing.T) {
	env := newTestShopEnv()

	cards := env.svc.buildFactionCards("p1", "SD")

	// Expected: card 1, 2, 3, 4 (card 5 inactive → excluded)
	if len(cards) != 4 {
		t.Fatalf("expected 4 SD card entries, got %d", len(cards))
	}

	counts := make(map[int64]int)
	for _, c := range cards {
		counts[c.CardNo] = c.Count
	}
	if counts[1] != 3 {
		t.Errorf("card 1 (unlimited): expected count=3, got %d", counts[1])
	}
	if counts[3] != 3 {
		t.Errorf("card 3 (limited): expected count=3, got %d", counts[3])
	}
	if counts[4] != 3 {
		t.Errorf("card 4 (semi_limited): expected count=3, got %d", counts[4])
	}
	if counts[5] != 0 {
		t.Errorf("card 5 (inactive): expected count=0, got %d", counts[5])
	}
}

func TestBuildFactionCards_Neutral(t *testing.T) {
	env := newTestShopEnv()

	cards := env.svc.buildFactionCards("p1", "Neutral")
	if len(cards) != 2 {
		t.Fatalf("expected 2 Neutral card entries, got %d", len(cards))
	}

	total := 0
	for _, c := range cards {
		total += c.Count
	}
	if total != 6 {
		t.Errorf("expected 6 total Neutral copies, got %d", total)
	}
}

func TestBuildFactionCards_UnknownFaction(t *testing.T) {
	env := newTestShopEnv()

	cards := env.svc.buildFactionCards("p1", "Unknown")
	if len(cards) != 0 {
		t.Errorf("expected 0 cards for unknown faction, got %d", len(cards))
	}
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 1 {
		t.Errorf("expected 1 card entry, got %d", len(cards))
	}
	if len(cards) > 0 && cards[0].Count != 3 {
		t.Errorf("expected count=3, got %d", cards[0].Count)
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
	if err != nil {
		t.Fatalf("idempotent purchase failed: %v", err)
	}

	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 1 {
		t.Errorf("expected 1 card entry (no duplication), got %d", len(cards))
	}
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
	if !errors.Is(err, ErrReceiptVerificationFailed) {
		t.Errorf("expected ErrReceiptVerificationFailed, got: %v", err)
	}

	cards := env.shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPurchase_InactiveProduct(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "old_product",
		Name:      "旧商品",
		Type:      model.ProductTypeFactionSet,
		Price:     100,
		Content:   json.RawMessage(`{"faction":"SD"}`),
		IsActive:  false,
	})

	err := env.svc.Purchase(context.Background(), "p1", "old_product", "ios", "receipt-1")
	if !errors.Is(err, ErrProductNotActive) {
		t.Errorf("expected ErrProductNotActive, got: %v", err)
	}
}

func TestPurchase_UnsupportedPlatform(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_sd",
		Name:      "SDカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SD"}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "faction_sd", "windows", "receipt-1")
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("expected ErrUnsupportedPlatform, got: %v", err)
	}
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil expiry time")
	}

	// Verify player is now premium
	p, _ := env.playerRepo.FindByID(context.Background(), "p1")
	if !p.IsPremium {
		t.Error("expected player to be premium")
	}
	if p.PremiumExpiresAt == nil {
		t.Error("expected PremiumExpiresAt to be set")
	}

	// Verify subscription was created
	sub, _ := env.shopRepo.GetActiveSubscription(context.Background(), "p1")
	if sub == nil {
		t.Fatal("expected active subscription to be created")
	}
	if sub.Status != model.SubscriptionStatusActive {
		t.Errorf("expected status active, got %s", sub.Status)
	}
}

func TestSubscribe_NotSubscriptionProduct(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_sd",
		Name:      "SDカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SD"}`),
		IsActive:  true,
	})

	_, err := env.svc.Subscribe(context.Background(), "p1", "faction_sd", "ios", "sub-token-1")
	if !errors.Is(err, ErrProductNotSubscription) {
		t.Errorf("expected ErrProductNotSubscription, got: %v", err)
	}
}

func TestSubscribe_VerificationFailed(t *testing.T) {
	env := newTestShopEnv()

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

	_, err := env.svc.Subscribe(context.Background(), "p1", "premium_monthly", "ios", "bad-sub-token")
	if !errors.Is(err, ErrSubVerificationFailed) {
		t.Errorf("expected ErrSubVerificationFailed, got: %v", err)
	}
}

func TestSubscribe_UnsupportedPlatform(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "premium_monthly",
		Name:      "プレミアム月額",
		Type:      model.ProductTypeSubscription,
		Price:     480,
		Content:   json.RawMessage(`{}`),
		IsActive:  true,
	})

	_, err := env.svc.Subscribe(context.Background(), "p1", "premium_monthly", "windows", "sub-token-1")
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("expected ErrUnsupportedPlatform, got: %v", err)
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil expiry time")
	}
}

// ---------------------------------------------------------------------------
// GetProducts tests
// ---------------------------------------------------------------------------

func TestGetProducts_WithOwnership(t *testing.T) {
	env := newTestShopEnv()

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_sd",
		Name:      "SDカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SD"}`),
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
		ProductID:     "faction_sd",
		Platform:      "ios",
		PurchaseToken: "test-token-sd",
		PurchasedAt:   time.Now(),
	}, nil)

	products, err := env.svc.GetProducts(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}

	owned := 0
	for _, p := range products {
		if p.IsOwned {
			owned++
		}
	}
	if owned != 1 {
		t.Errorf("expected 1 owned product, got %d", owned)
	}
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
	_ = env.shopRepo.CreateSubscription(context.Background(), &model.Subscription{
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if !products[0].IsOwned {
		t.Error("expected subscription product to be owned")
	}
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
		{"sd", "SD", true},
		{"SD", "SD", true},
		{"tenki", "Tenki", true},
		{"TENKI", "Tenki", true},
		{"sugar", "Sugar", true},
		{"tuners", "Tuners", true},
		{"neutral", "Neutral", true},
		{"invalid", "invalid", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := normalizeFaction(tt.input)
		if got != tt.expected || ok != tt.ok {
			t.Errorf("normalizeFaction(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.expected, tt.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// getVerifier tests
// ---------------------------------------------------------------------------

func TestGetVerifier(t *testing.T) {
	env := newTestShopEnv()

	if v := env.svc.getVerifier("ios"); v == nil {
		t.Error("expected non-nil verifier for ios")
	}
	if v := env.svc.getVerifier("android"); v == nil {
		t.Error("expected non-nil verifier for android")
	}
	if v := env.svc.getVerifier("windows"); v != nil {
		t.Error("expected nil verifier for unsupported platform")
	}
}

// ---------------------------------------------------------------------------
// Additional tests
// ---------------------------------------------------------------------------

func TestSelectFaction_PlayerNotFound(t *testing.T) {
	env := newTestShopEnv()
	// Do NOT create the player — "nonexistent" has no record in playerRepo.

	_, err := env.svc.SelectFaction(context.Background(), "nonexistent", "sd")
	if err == nil {
		t.Fatal("expected error for nonexistent player, got nil")
	}
}

func TestPurchase_VerifierReturnsError(t *testing.T) {
	env := newTestShopEnv()

	env.svc.appleVerifier = &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return nil, fmt.Errorf("network timeout")
		},
	}

	env.shopRepo.AddProduct(&model.Product{
		ProductID: "faction_sd",
		Name:      "SDカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SD"}`),
		IsActive:  true,
	})

	err := env.svc.Purchase(context.Background(), "p1", "faction_sd", "ios", "receipt-err")
	if err == nil {
		t.Fatal("expected error when verifier returns error, got nil")
	}
	if !strings.Contains(err.Error(), "verify receipt:") {
		t.Errorf("expected error to contain 'verify receipt:', got: %v", err)
	}
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
	if err == nil {
		t.Fatal("expected error for subscription product via Purchase, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported product type") {
		t.Errorf("expected error to contain 'unsupported product type', got: %v", err)
	}
}

func TestSelectFaction_CardCopiesVerified(t *testing.T) {
	env := newTestShopEnv()
	createTestPlayer(env, "p1")

	_, err := env.svc.SelectFaction(context.Background(), "p1", "sd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cards := env.shopRepo.GetPlayerCardsForTest("p1")

	// Build a map of cardNo -> count for easy lookup.
	counts := make(map[int64]int)
	for _, c := range cards {
		counts[c.CardNo] = c.Count
	}

	// All cards get 3 copies regardless of restriction.
	// SD cards: 1, 2, 3, 4; Neutral cards: 100, 101
	expected := map[int64]int{
		1:   3,
		2:   3,
		3:   3,
		4:   3,
		100: 3,
		101: 3,
	}

	for cardNo, wantCount := range expected {
		if gotCount, ok := counts[cardNo]; !ok {
			t.Errorf("card %d not found in player cards", cardNo)
		} else if gotCount != wantCount {
			t.Errorf("card %d: expected count=%d, got %d", cardNo, wantCount, gotCount)
		}
	}
}
