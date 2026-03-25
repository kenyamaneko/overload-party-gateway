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
		IsPremium:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	dailyBattle := &model.PlayerDailyBattle{
		PlayerID:         player.PlayerID,
		DailyBattleCount: 0,
		LastResetDate:    civil.DateOf(time.Now().UTC()),
	}

	// TODO: プレイヤー作成・設定・スタンプ付与を単一トランザクションにまとめる。
	//       現状はリポジトリが個別に pool.Begin() しているため、途中で失敗すると
	//       プレイヤー行だけ残る不整合が起きる。
	if err := s.playerRepo.Create(ctx, player, dailyBattle); err != nil {
		return nil, fmt.Errorf("create player: %w", err)
	}

	settings := &model.UserSettings{
		PlayerID:    player.PlayerID,
		Language:    DefaultLanguage,
		BgmVolume:   DefaultBgmVolume,
		SeVolume:    DefaultSeVolume,
		PushEnabled: DefaultPushEnabled,
		UpdatedAt:   time.Now(),
	}
	if err := s.userSettingsRepo.Upsert(ctx, settings); err != nil {
		return nil, fmt.Errorf("create default user settings for player %s: %w", player.PlayerID, err)
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
	if err := s.shopRepo.InsertPlayerItems(ctx, items); err != nil {
		return nil, fmt.Errorf("grant starter stamps for player %s: %w", player.PlayerID, err)
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
