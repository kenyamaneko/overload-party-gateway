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
	// Given
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})

	// When
	player, err := svc.Register(context.Background(), "firebase-uid-1", "TestUser")

	// Then: returns player with correct fields
	require.NoError(t, err)
	assert.NotEmpty(t, player.PlayerID)
	assert.Equal(t, "firebase-uid-1", player.FirebaseUID)
	assert.Equal(t, "TestUser", player.Username)
	assert.Equal(t, int64(1), player.Level)
	assert.Equal(t, int64(0), player.Exp)
	assert.False(t, player.IsPremium)

	// Then: persists player to repository
	found, err := playerRepo.FindByFirebaseUID(context.Background(), "firebase-uid-1")
	require.NoError(t, err)
	assert.Equal(t, player.PlayerID, found.PlayerID)

	// Then: initializes daily battle record
	daily, err := playerRepo.GetDailyBattle(context.Background(), player.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), daily.DailyBattleCount)

	// Then: creates default user settings
	settings, err := userSettingsRepo.Get(context.Background(), player.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, "ja", settings.Language)
	assert.Equal(t, int64(50), settings.BgmVolume)
	assert.Equal(t, int64(50), settings.SeVolume)
	assert.True(t, settings.PushEnabled)

	// Then: grants initial stamps (1-7)
	require.Len(t, shopRepo.insertedItems, 7)
	for i, item := range shopRepo.insertedItems {
		assert.Equal(t, "stamp", item.ItemType)
		assert.Equal(t, int64(i+1), item.ItemNo)
		assert.Equal(t, player.PlayerID, item.PlayerID)
	}
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})

	_, err := svc.Register(context.Background(), "firebase-uid-dup", "FirstUser")
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), "firebase-uid-dup", "SecondUser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "player already registered")
}

func TestRegister_StampFailure_Fatal(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := &failingStampShopRepo{MockShopRepository: repository.NewMockShopRepository()}
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})

	_, err := svc.Register(context.Background(), "firebase-uid-stamp-fail", "StampFailUser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grant starter stamps")
}

func TestLogin_Success(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})

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

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})

	_, err := svc.Login(context.Background(), "nonexistent-uid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "player not found")
}
