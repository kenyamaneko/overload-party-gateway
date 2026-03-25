package service

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/civil"
	"github.com/google/uuid"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const starterStampCount = int64(7)


// ユーザー設定の初期値。登録時および設定未取得時のフォールバックに使用。
const (
	DefaultLanguage    = "ja"
	DefaultBgmVolume   = int64(50)
	DefaultSeVolume    = int64(50)
	DefaultPushEnabled = true
)

type AuthService struct {
	playerRepo       repository.PlayerRepo
	shopRepo         repository.ShopRepository
	userSettingsRepo repository.UserSettingsRepo
	txRunner         repository.TxRunner
}

func NewAuthService(playerRepo repository.PlayerRepo, shopRepo repository.ShopRepository, userSettingsRepo repository.UserSettingsRepo, txRunner repository.TxRunner) *AuthService {
	return &AuthService{playerRepo: playerRepo, shopRepo: shopRepo, userSettingsRepo: userSettingsRepo, txRunner: txRunner}
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
		IsPremium:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	dailyBattle := &model.PlayerDailyBattle{
		PlayerID:         player.PlayerID,
		DailyBattleCount: 0,
		LastResetDate:    civil.DateOf(time.Now().UTC()),
	}

	settings := &model.UserSettings{
		PlayerID:    player.PlayerID,
		Language:    DefaultLanguage,
		BgmVolume:   DefaultBgmVolume,
		SeVolume:    DefaultSeVolume,
		PushEnabled: DefaultPushEnabled,
		UpdatedAt:   now,
	}

	var items []*model.PlayerItem
	for i := int64(1); i <= starterStampCount; i++ {
		items = append(items, &model.PlayerItem{
			PlayerID:   player.PlayerID,
			ItemType:   model.ItemTypeStamp,
			ItemNo:     i,
			AcquiredAt: now,
		})
	}

	if err := s.txRunner.RunInTx(ctx, func(tx repository.DBTX) error {
		if err := s.playerRepo.CreateWithTx(ctx, tx, player, dailyBattle); err != nil {
			return fmt.Errorf("create player: %w", err)
		}
		if err := s.userSettingsRepo.UpsertWithTx(ctx, tx, settings); err != nil {
			return fmt.Errorf("create default user settings: %w", err)
		}
		if err := s.shopRepo.InsertPlayerItemsWithTx(ctx, tx, items); err != nil {
			return fmt.Errorf("grant starter stamps: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
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
