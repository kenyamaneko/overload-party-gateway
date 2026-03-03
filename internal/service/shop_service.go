package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-common/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

type ShopService struct {
	shopRepo       repository.ShopRepository
	playerRepo     repository.PlayerRepo
	cardCache      *cache.CardCache
	appleVerifier  platform.ReceiptVerifier
	googleVerifier platform.ReceiptVerifier
}

func NewShopService(
	shopRepo repository.ShopRepository,
	playerRepo repository.PlayerRepo,
	cardCache *cache.CardCache,
	appleVerifier platform.ReceiptVerifier,
	googleVerifier platform.ReceiptVerifier,
) *ShopService {
	return &ShopService{
		shopRepo:       shopRepo,
		playerRepo:     playerRepo,
		cardCache:      cardCache,
		appleVerifier:  appleVerifier,
		googleVerifier: googleVerifier,
	}
}

// SelectFaction handles the initial faction selection for a new player.
// It grants all cards from the selected faction + Neutral to the player.
func (s *ShopService) SelectFaction(ctx context.Context, playerID, faction string) (int, error) {
	normalized, ok := model.NormalizeFaction(faction)
	if !ok || !model.ValidFactions[normalized] {
		return 0, fmt.Errorf("invalid faction: %s", faction)
	}
	faction = normalized

	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return 0, fmt.Errorf("find player: %w", err)
	}
	if player.SelectedFaction != nil {
		return 0, fmt.Errorf("faction already selected")
	}

	factionCards := s.buildFactionCards(playerID, faction)
	neutralCards := s.buildFactionCards(playerID, model.FactionNeutral)
	allCards := append(factionCards, neutralCards...)

	if err := s.shopRepo.InsertPlayerCards(ctx, allCards); err != nil {
		return 0, fmt.Errorf("insert player cards: %w", err)
	}
	if err := s.shopRepo.UpdatePlayerFaction(ctx, playerID, faction); err != nil {
		return 0, fmt.Errorf("update player faction: %w", err)
	}

	return len(allCards), nil
}

// ProductWithOwnership is a product with ownership info for a specific player.
type ProductWithOwnership struct {
	model.Product
	IsOwned bool `json:"is_owned"`
}

// GetProducts returns all active products with ownership info for the player.
func (s *ShopService) GetProducts(ctx context.Context, playerID string) ([]ProductWithOwnership, error) {
	products, err := s.shopRepo.GetActiveProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("get products: %w", err)
	}

	ownedFactions, err := s.shopRepo.GetPlayerOwnedFactions(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get owned factions: %w", err)
	}
	ownedFactionSet := make(map[string]bool)
	for _, f := range ownedFactions {
		ownedFactionSet[f] = true
	}

	activeSub, err := s.shopRepo.GetActiveSubscription(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}

	var result []ProductWithOwnership
	for _, p := range products {
		owned := false
		switch p.Type {
		case model.ProductTypeFactionSet:
			var content model.FactionSetContent
			if err := json.Unmarshal(p.Content, &content); err == nil {
				owned = ownedFactionSet[content.Faction]
			}
		case model.ProductTypeSubscription:
			owned = activeSub != nil
		}
		result = append(result, ProductWithOwnership{
			Product: *p,
			IsOwned: owned,
		})
	}
	return result, nil
}

// Purchase processes a one-time purchase (faction set or cosmetic).
func (s *ShopService) Purchase(ctx context.Context, playerID, productID, pf, purchaseToken string) error {
	// Idempotency check
	existing, err := s.shopRepo.FindPurchaseByToken(ctx, playerID, purchaseToken)
	if err != nil {
		return fmt.Errorf("check existing purchase: %w", err)
	}
	if existing != nil {
		return nil // Already processed
	}

	product, err := s.shopRepo.GetProductByID(ctx, productID)
	if err != nil {
		return fmt.Errorf("get product: %w", err)
	}
	if !product.IsActive {
		return fmt.Errorf("product is not active")
	}

	// Verify receipt
	verifier := s.getVerifier(pf)
	if verifier == nil {
		return fmt.Errorf("unsupported platform: %s", pf)
	}
	result, err := verifier.VerifyPurchase(ctx, purchaseToken)
	if err != nil {
		return fmt.Errorf("verify receipt: %w", err)
	}
	if !result.IsValid {
		return fmt.Errorf("receipt verification failed")
	}

	purchase := &model.OneTimePurchase{
		PlayerID:      playerID,
		ProductID:     productID,
		Platform:      pf,
		PurchaseToken: purchaseToken,
		PurchasedAt:   time.Now(),
	}

	switch product.Type {
	case model.ProductTypeFactionSet:
		var content model.FactionSetContent
		if err := json.Unmarshal(product.Content, &content); err != nil {
			return fmt.Errorf("parse faction set content: %w", err)
		}
		cards := s.buildFactionCards(playerID, content.Faction)
		if err := s.shopRepo.CreatePurchaseWithCards(ctx, purchase, cards); err != nil {
			return fmt.Errorf("create purchase with cards: %w", err)
		}

	case model.ProductTypeCosmetic:
		var content model.CosmeticContent
		if err := json.Unmarshal(product.Content, &content); err != nil {
			return fmt.Errorf("parse cosmetic content: %w", err)
		}
		item := &model.PlayerItem{
			PlayerID:   playerID,
			ItemType:   content.ItemType,
			ItemNo:     content.ItemNo,
			AcquiredAt: time.Now(),
		}
		if err := s.shopRepo.CreatePurchaseWithItem(ctx, purchase, item); err != nil {
			return fmt.Errorf("create purchase with item: %w", err)
		}

	default:
		return fmt.Errorf("unsupported product type for purchase: %s", product.Type)
	}

	return nil
}

// Subscribe starts a premium subscription.
func (s *ShopService) Subscribe(ctx context.Context, playerID, productID, pf, purchaseToken string) (*time.Time, error) {
	product, err := s.shopRepo.GetProductByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	if product.Type != model.ProductTypeSubscription {
		return nil, fmt.Errorf("product is not a subscription")
	}

	verifier := s.getVerifier(pf)
	if verifier == nil {
		return nil, fmt.Errorf("unsupported platform: %s", pf)
	}
	info, err := verifier.VerifySubscription(ctx, purchaseToken)
	if err != nil {
		return nil, fmt.Errorf("verify subscription: %w", err)
	}
	if !info.IsValid {
		return nil, fmt.Errorf("subscription verification failed")
	}

	now := time.Now()
	sub := &model.Subscription{
		PlayerID:           playerID,
		ProductID:          productID,
		Platform:           pf,
		PurchaseToken:      purchaseToken,
		Status:             model.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   info.ExpiresAt,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := s.shopRepo.CreateSubscription(ctx, sub); err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}

	if err := s.shopRepo.UpdatePlayerPremium(ctx, playerID, true, &info.ExpiresAt); err != nil {
		return nil, fmt.Errorf("update player premium: %w", err)
	}

	return &info.ExpiresAt, nil
}

// buildFactionCards generates PlayerCard records for all cards in the given faction,
// respecting restriction-based copy limits.
func (s *ShopService) buildFactionCards(playerID string, faction string) []*model.PlayerCard {
	var cards []*model.PlayerCard
	for _, card := range s.cardCache.All() {
		if card.Faction != faction || !card.IsActive {
			continue
		}
		copies := model.RestrictionCopyCount(card.Restriction)
		cards = append(cards, &model.PlayerCard{
			PlayerID:            playerID,
			CardNo:              card.CardNo,
			IllustrationVariant: 0,
			Count:               copies,
		})
	}
	return cards
}

func (s *ShopService) getVerifier(pf string) platform.ReceiptVerifier {
	switch pf {
	case model.PlatformIOS:
		return s.appleVerifier
	case model.PlatformAndroid:
		return s.googleVerifier
	default:
		return nil
	}
}
