package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stampTrackingShopRepo wraps MockShopRepository to record inserted player items.
type stampTrackingShopRepo struct {
	*repository.MockShopRepository
	insertedItems []*model.PlayerItem
}

func newStampTrackingShopRepo() *stampTrackingShopRepo {
	return &stampTrackingShopRepo{
		MockShopRepository: repository.NewMockShopRepository(),
	}
}

func (r *stampTrackingShopRepo) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	r.insertedItems = append(r.insertedItems, items...)
	return nil
}

// failingStampShopRepo wraps MockShopRepository but returns an error from InsertPlayerItems.
type failingStampShopRepo struct {
	*repository.MockShopRepository
}

func (r *failingStampShopRepo) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	return fmt.Errorf("database connection lost")
}

func TestRegister_Success(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	player, err := svc.Register(context.Background(), "firebase-uid-1", "TestUser")
	require.NoError(t, err)

	assert.NotEmpty(t, player.PlayerID)
	assert.Equal(t, "firebase-uid-1", player.FirebaseUID)
	assert.Equal(t, "TestUser", player.Username)
	assert.Equal(t, int64(1), player.Level)
	assert.Equal(t, int64(0), player.Exp)
	assert.Equal(t, int64(0), player.Wins)
	assert.Equal(t, int64(0), player.Losses)
	assert.False(t, player.IsPremium)

	found, err := playerRepo.FindByFirebaseUID(context.Background(), "firebase-uid-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, player.PlayerID, found.PlayerID)

	daily, err := playerRepo.GetDailyBattle(context.Background(), player.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), daily.DailyBattleCount)

	settings, err := userSettingsRepo.Get(context.Background(), player.PlayerID)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "ja", settings.Language)
	assert.Equal(t, int64(50), settings.BgmVolume)
	assert.Equal(t, int64(50), settings.SeVolume)
	assert.True(t, settings.PushEnabled)

	require.Len(t, shopRepo.insertedItems, 7)
	for i, item := range shopRepo.insertedItems {
		expectedNo := int64(i + 1)
		assert.Equal(t, "stamp", item.ItemType)
		assert.Equal(t, expectedNo, item.ItemNo)
		assert.Equal(t, player.PlayerID, item.PlayerID)
	}
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	_, err := svc.Register(context.Background(), "firebase-uid-dup", "FirstUser")
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), "firebase-uid-dup", "SecondUser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "player already registered")
}

func TestRegister_StampFailure_NotFatal(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := &failingStampShopRepo{MockShopRepository: repository.NewMockShopRepository()}
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	player, err := svc.Register(context.Background(), "firebase-uid-stamp-fail", "StampFailUser")
	require.NoError(t, err)

	assert.NotEmpty(t, player.PlayerID)
	assert.Equal(t, "StampFailUser", player.Username)

	found, err := playerRepo.FindByFirebaseUID(context.Background(), "firebase-uid-stamp-fail")
	require.NoError(t, err)
	require.NotNil(t, found)

	settings, err := userSettingsRepo.Get(context.Background(), player.PlayerID)
	require.NoError(t, err)
	require.NotNil(t, settings)
}

func TestLogin_Success(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	registered, err := svc.Register(context.Background(), "firebase-uid-login", "LoginUser")
	require.NoError(t, err)

	player, err := svc.Login(context.Background(), "firebase-uid-login")
	require.NoError(t, err)
	assert.Equal(t, registered.PlayerID, player.PlayerID)
	assert.Equal(t, "LoginUser", player.Username)
}

func TestLogin_PlayerNotFound(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	_, err := svc.Login(context.Background(), "nonexistent-uid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "player not found")
}
