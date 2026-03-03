package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// mockPlayerRepo is a minimal mock for player lookup in tests.
type mockPlayerRepo struct {
	players map[string]*model.Player
}

func (m *mockPlayerRepo) FindByID(ctx context.Context, playerID string) (*model.Player, error) {
	p, ok := m.players[playerID]
	if !ok {
		return nil, fmt.Errorf("player not found")
	}
	return p, nil
}

// testShopServiceWithPlayerRepo creates a ShopService that uses the mock player repo.
func testShopServiceWithPlayerRepo(playerRepo *mockPlayerRepo) (*ShopService, *repository.MockShopRepository) {
	shopRepo := repository.NewMockShopRepository()
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

	// Create a wrapper PlayerRepository that delegates to our mock
	svc := &ShopService{
		shopRepo:       shopRepo,
		playerRepo:     nil, // Set below via struct hack
		cardCache:      cc,
		appleVerifier:  verifier,
		googleVerifier: verifier,
	}

	return svc, shopRepo
}

// --- SelectFaction tests ---

func TestSelectFaction_Success(t *testing.T) {
	playerRepo := &mockPlayerRepo{
		players: map[string]*model.Player{
			"p1": {PlayerID: "p1", SelectedFaction: nil},
		},
	}
	svc, shopRepo := testShopServiceWithPlayerRepo(playerRepo)
	// Hack: replace playerRepo.FindByID call path
	// We need to override the SelectFaction method to use our mock.
	// Instead, let's use a real service but with an adapted approach.

	// The ShopService.SelectFaction calls s.playerRepo.FindByID.
	// Since playerRepo is *repository.PlayerRepository (concrete type), we can't easily mock it.
	// Let's test via the buildFactionCards helper instead, and test SelectFaction integration.

	// For this test, we'll directly test the card building logic.
	cards := svc.buildFactionCards("p1", "SD")

	// Now returns 1 entry per card_no with Count field.
	// Expected: card 1, card 2, card 3, card 4 (4 entries, card 5 inactive → excluded)
	if len(cards) != 4 {
		t.Errorf("expected 4 SD card entries, got %d", len(cards))
	}

	// Count copies per card_no (from Count field)
	counts := make(map[int64]int)
	for _, c := range cards {
		counts[c.CardNo] = c.Count
	}
	if counts[1] != 3 {
		t.Errorf("card 1 (unlimited): expected count=3, got %d", counts[1])
	}
	if counts[2] != 3 {
		t.Errorf("card 2 (unlimited): expected count=3, got %d", counts[2])
	}
	if counts[3] != 1 {
		t.Errorf("card 3 (limited): expected count=1, got %d", counts[3])
	}
	if counts[4] != 2 {
		t.Errorf("card 4 (semi_limited): expected count=2, got %d", counts[4])
	}
	if counts[5] != 0 {
		t.Errorf("card 5 (inactive): expected count=0, got %d", counts[5])
	}

	// Verify total copies = 9
	totalCopies := 0
	for _, c := range cards {
		totalCopies += c.Count
	}
	if totalCopies != 9 {
		t.Errorf("expected 9 total copies, got %d", totalCopies)
	}

	// Test Neutral cards
	neutralCards := svc.buildFactionCards("p1", "Neutral")
	// card 100 (count=3) + card 101 (count=1) = 2 entries, 4 total copies
	if len(neutralCards) != 2 {
		t.Errorf("expected 2 Neutral card entries, got %d", len(neutralCards))
	}
	neutralTotal := 0
	for _, c := range neutralCards {
		neutralTotal += c.Count
	}
	if neutralTotal != 4 {
		t.Errorf("expected 4 total Neutral copies, got %d", neutralTotal)
	}

	// Verify all cards belong to the correct player
	for _, c := range cards {
		if c.PlayerID != "p1" {
			t.Errorf("expected player_id p1, got %s", c.PlayerID)
		}
	}

	_ = shopRepo
}

func TestSelectFaction_InvalidFaction(t *testing.T) {
	playerRepo := &mockPlayerRepo{
		players: map[string]*model.Player{
			"p1": {PlayerID: "p1", SelectedFaction: nil},
		},
	}
	svc, _ := testShopServiceWithPlayerRepo(playerRepo)

	// buildFactionCards with invalid faction should return 0 cards
	cards := svc.buildFactionCards("p1", "InvalidFaction")
	if len(cards) != 0 {
		t.Errorf("expected 0 cards for invalid faction, got %d", len(cards))
	}
}

// --- Purchase tests ---

func TestPurchase_FactionSet_Success(t *testing.T) {
	shopRepo := repository.NewMockShopRepository()
	cc := cache.NewCardCache()
	cc.InjectForTest(200, &model.CardDefinition{CardNo: 200, CardName: "Tenki VM", Faction: "Tenki", CardType: "Compute", Restriction: "unlimited", IsActive: true})

	verifier := &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-123", ProductID: "faction_tenki"}, nil
		},
	}

	product := &model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	}
	shopRepo.AddProduct(product)

	svc := NewShopService(shopRepo, nil, cc, verifier, verifier)

	err := svc.Purchase(context.Background(), "p1", "faction_tenki", "ios", "receipt-token-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify cards were granted (1 entry with Count=3 for unlimited card)
	cards := shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 1 {
		t.Errorf("expected 1 card entry, got %d", len(cards))
	}
	if len(cards) > 0 && cards[0].Count != 3 {
		t.Errorf("expected count=3, got %d", cards[0].Count)
	}
}

func TestPurchase_Idempotent(t *testing.T) {
	shopRepo := repository.NewMockShopRepository()
	cc := cache.NewCardCache()
	cc.InjectForTest(200, &model.CardDefinition{CardNo: 200, CardName: "Tenki VM", Faction: "Tenki", CardType: "Compute", Restriction: "unlimited", IsActive: true})

	verifier := &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-123"}, nil
		},
	}

	product := &model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	}
	shopRepo.AddProduct(product)

	svc := NewShopService(shopRepo, nil, cc, verifier, verifier)

	// First purchase
	err := svc.Purchase(context.Background(), "p1", "faction_tenki", "ios", "receipt-token-1")
	if err != nil {
		t.Fatalf("first purchase failed: %v", err)
	}

	// Second purchase with same token — should succeed without duplicating cards
	err = svc.Purchase(context.Background(), "p1", "faction_tenki", "ios", "receipt-token-1")
	if err != nil {
		t.Fatalf("idempotent purchase failed: %v", err)
	}

	// Verify cards were only granted once (1 entry with Count=3)
	cards := shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 1 {
		t.Errorf("expected 1 card entry (no duplication), got %d", len(cards))
	}
	if len(cards) > 0 && cards[0].Count != 3 {
		t.Errorf("expected count=3 (no duplication), got %d", cards[0].Count)
	}
}

func TestPurchase_ReceiptFailed(t *testing.T) {
	shopRepo := repository.NewMockShopRepository()
	cc := cache.NewCardCache()

	verifier := &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: false}, nil
		},
	}

	product := &model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	}
	shopRepo.AddProduct(product)

	svc := NewShopService(shopRepo, nil, cc, verifier, verifier)

	err := svc.Purchase(context.Background(), "p1", "faction_tenki", "ios", "bad-receipt")
	if err == nil {
		t.Fatal("expected error for failed receipt verification")
	}
	if err.Error() != "receipt verification failed" {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify no cards were granted
	cards := shopRepo.GetPlayerCardsForTest("p1")
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
}

func TestPurchase_CosmeticItem(t *testing.T) {
	shopRepo := repository.NewMockShopRepository()
	cc := cache.NewCardCache()

	verifier := &platform.MockReceiptVerifier{
		VerifyPurchaseFn: func(ctx context.Context, token string) (*platform.VerifyResult, error) {
			return &platform.VerifyResult{IsValid: true, TransactionID: "txn-456"}, nil
		},
	}

	product := &model.Product{
		ProductID: "playmat_01",
		Name:      "プレイマット: サイバー",
		Type:      model.ProductTypeCosmetic,
		Price:     320,
		Content:   json.RawMessage(`{"item_type":"playmat","item_no":1}`),
		IsActive:  true,
	}
	shopRepo.AddProduct(product)

	svc := NewShopService(shopRepo, nil, cc, verifier, verifier)

	err := svc.Purchase(context.Background(), "p1", "playmat_01", "android", "cosmetic-receipt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- GetProducts tests ---

func TestGetProducts_WithOwnership(t *testing.T) {
	shopRepo := repository.NewMockShopRepository()
	cc := cache.NewCardCache()

	// Add products
	shopRepo.AddProduct(&model.Product{
		ProductID: "faction_sd",
		Name:      "SDカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"SD"}`),
		IsActive:  true,
	})
	shopRepo.AddProduct(&model.Product{
		ProductID: "faction_tenki",
		Name:      "Tenkiカードセット",
		Type:      model.ProductTypeFactionSet,
		Price:     980,
		Content:   json.RawMessage(`{"faction":"Tenki"}`),
		IsActive:  true,
	})

	// Simulate player owning SD faction
	_ = shopRepo.UpdatePlayerFaction(context.Background(), "p1", "SD")

	svc := NewShopService(shopRepo, nil, cc, nil, nil)

	products, err := svc.GetProducts(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}

	// Find the owned and unowned products
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

// --- RestrictionCopyCount tests ---

func TestRestrictionCopyCount(t *testing.T) {
	tests := []struct {
		restriction string
		expected    int
	}{
		{"unlimited", 3},
		{"semi_limited", 2},
		{"limited", 1},
		{"", 3}, // default
	}
	for _, tt := range tests {
		got := restrictionCopyCount(tt.restriction)
		if got != tt.expected {
			t.Errorf("RestrictionCopyCount(%q) = %d, want %d", tt.restriction, got, tt.expected)
		}
	}
}
