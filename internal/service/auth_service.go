package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/civil"
	"github.com/google/uuid"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const starterStampCount = int64(7)

type AuthService struct {
	playerRepo       repository.PlayerRepo
	shopRepo         repository.ShopRepository
	userSettingsRepo repository.UserSettingsRepo
}

func NewAuthService(playerRepo repository.PlayerRepo, shopRepo repository.ShopRepository, userSettingsRepo repository.UserSettingsRepo) *AuthService {
	return &AuthService{playerRepo: playerRepo, shopRepo: shopRepo, userSettingsRepo: userSettingsRepo}
}

func (s *AuthService) Register(ctx context.Context, firebaseUID, username string) (*model.Player, error) {
	existing, err := s.playerRepo.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		return nil, fmt.Errorf("check existing player: %w", err)
	}
	if existing != nil {
		return nil, ErrPlayerAlreadyRegistered
	}

	now := time.Now()
	player := &model.Player{
		PlayerID:    uuid.New().String(),
		FirebaseUID: firebaseUID,
		Username:    username,
		Level:       1,
		Exp:         0,
		Wins:        0,
		Losses:      0,
		IsPremium:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	dailyBattle := &model.PlayerDailyBattle{
		PlayerID:         player.PlayerID,
		DailyBattleCount: 0,
		LastResetDate:    civil.DateOf(time.Now().UTC()),
	}

	if err := s.playerRepo.Create(ctx, player, dailyBattle); err != nil {
		return nil, fmt.Errorf("create player: %w", err)
	}

	// Create default user settings. Failure does not roll back player creation.
	settings := &model.UserSettings{
		PlayerID:    player.PlayerID,
		Language:    "ja",
		BgmVolume:   50,
		SeVolume:    50,
		PushEnabled: true,
		UpdatedAt:   time.Now(),
	}
	if err := s.userSettingsRepo.Upsert(ctx, settings); err != nil {
		log.Printf("warn: failed to create default user settings for player %s: %v", player.PlayerID, err)
	}

	// Grant starter stamps (1–7). Failure does not roll back player creation.
	var items []*model.PlayerItem
	for i := int64(1); i <= starterStampCount; i++ {
		items = append(items, &model.PlayerItem{
			PlayerID:   player.PlayerID,
			ItemType:   "stamp",
			ItemNo:     i,
			AcquiredAt: now,
		})
	}
	if err := s.shopRepo.InsertPlayerItems(ctx, items); err != nil {
		log.Printf("warn: failed to grant starter stamps for player %s: %v", player.PlayerID, err)
	}

	return player, nil
}

func (s *AuthService) Login(ctx context.Context, firebaseUID string) (*model.Player, error) {
	player, err := s.playerRepo.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		return nil, fmt.Errorf("find player: %w", err)
	}
	if player == nil {
		return nil, ErrPlayerNotFound
	}
	return player, nil
}
