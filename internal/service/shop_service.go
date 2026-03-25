package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

var validFactions = map[string]bool{
	constants.FactionSHE:    true,
	constants.FactionTenki:  true,
	constants.FactionSugar:  true,
	constants.FactionTuners: true,
}

var normalizeFactionMap = map[string]string{
	"she":     constants.FactionSHE,
	"tenki":   constants.FactionTenki,
	"sugar":   constants.FactionSugar,
	"tuners":  constants.FactionTuners,
	"neutral": constants.FactionNeutral,
}

func normalizeFaction(s string) (string, bool) {
	if v, ok := normalizeFactionMap[strings.ToLower(s)]; ok {
		return v, true
	}
	return s, false
}

type ShopService struct {
	shopRepo       repository.ShopRepository
	playerRepo     repository.PlayerRepo
	factionRepo    repository.FactionRepo
	cardCache      *cache.CardCache
	appleVerifier  platform.ReceiptVerifier
	googleVerifier platform.ReceiptVerifier
}

func NewShopService(
	shopRepo repository.ShopRepository,
	playerRepo repository.PlayerRepo,
	factionRepo repository.FactionRepo,
	cardCache *cache.CardCache,
	appleVerifier platform.ReceiptVerifier,
	googleVerifier platform.ReceiptVerifier,
) *ShopService {
	return &ShopService{
		shopRepo:       shopRepo,
		playerRepo:     playerRepo,
		factionRepo:    factionRepo,
		cardCache:      cardCache,
		appleVerifier:  appleVerifier,
		googleVerifier: googleVerifier,
	}
}

// SelectFaction handles the initial faction selection for a new player.
// It grants all cards from the selected faction + Neutral to the player.
func (s *ShopService) SelectFaction(ctx context.Context, playerID, faction string) (int, error) {
	normalized, ok := normalizeFaction(faction)
	if !ok || !validFactions[normalized] {
		return 0, fmt.Errorf("%w: %s", ErrInvalidFaction, faction)
	}
	faction = normalized

	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return 0, fmt.Errorf("find player: %w", err)
	}
	if player == nil {
		return 0, fmt.Errorf("player %s not found", playerID)
	}
	if player.SelectedFaction != nil {
		return 0, ErrFactionAlreadySelected
	}

	factionCards := s.buildFactionCards(playerID, faction)
	neutralCards := s.buildFactionCards(playerID, constants.FactionNeutral)
	allCards := append(factionCards, neutralCards...)

	if err := s.shopRepo.InsertPlayerCards(ctx, allCards); err != nil {
		return 0, fmt.Errorf("insert player cards: %w", err)
	}
	if err := s.playerRepo.UpdateFaction(ctx, playerID, faction); err != nil {
		return 0, fmt.Errorf("update player faction: %w", err)
	}
	if err := s.factionRepo.AddPlayerFaction(ctx, playerID, faction, "initial_selection"); err != nil {
		return 0, fmt.Errorf("add player faction: %w", err)
	}

	return len(allCards), nil
}

type ProductWithOwnership struct {
	model.Product
	IsOwned bool `json:"is_owned"`
}

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

	result := make([]ProductWithOwnership, 0, len(products))
	for _, p := range products {
		owned := false
		switch p.Type {
		case model.ProductTypeFactionSet:
			var content model.FactionSetContent
			if err := json.Unmarshal(p.Content, &content); err != nil {
				log.Printf("failed to parse product content for %s: %v", p.ProductID, err)
				return nil, fmt.Errorf("parse product content for %s: %w", p.ProductID, err)
			}
			owned = ownedFactionSet[content.Faction]
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
		return ErrProductNotActive
	}

	verifier := s.getVerifier(pf)
	if verifier == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedPlatform, pf)
	}
	result, err := verifier.VerifyPurchase(ctx, purchaseToken)
	if err != nil {
		return fmt.Errorf("verify receipt: %w", err)
	}
	if !result.IsValid {
		return ErrReceiptVerificationFailed
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
		if err := s.factionRepo.AddPlayerFaction(ctx, playerID, content.Faction, "shop_purchase"); err != nil {
			return fmt.Errorf("add player faction: %w", err)
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

func (s *ShopService) Subscribe(ctx context.Context, playerID, productID, pf, purchaseToken string) (*time.Time, error) {
	product, err := s.shopRepo.GetProductByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	if product.Type != model.ProductTypeSubscription {
		return nil, ErrProductNotSubscription
	}

	verifier := s.getVerifier(pf)
	if verifier == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, pf)
	}
	info, err := verifier.VerifySubscription(ctx, purchaseToken)
	if err != nil {
		return nil, fmt.Errorf("verify subscription: %w", err)
	}
	if !info.IsValid {
		return nil, ErrSubVerificationFailed
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

	if err := s.playerRepo.UpdatePremium(ctx, playerID, true, &info.ExpiresAt); err != nil {
		return nil, fmt.Errorf("update player premium: %w", err)
	}

	return &info.ExpiresAt, nil
}

func (s *ShopService) buildFactionCards(playerID string, faction string) []*model.PlayerCard {
	var cards []*model.PlayerCard
	for _, card := range s.cardCache.All() {
		if card.Faction != faction || !card.IsActive {
			continue
		}
		cards = append(cards, &model.PlayerCard{
			PlayerID: playerID,
			CardID:   card.CardID,
			ArtNo:    0,
			Count:    3,
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
