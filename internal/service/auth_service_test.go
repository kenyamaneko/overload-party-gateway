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

func newAuthTestService() (*AuthService, *repository.MockShopRepository) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})
	return svc, shopRepo
}

func TestRegister_ReturnsPlayerWithCorrectFields(t *testing.T) {
	svc, _ := newAuthTestService()

	player, err := svc.Register(context.Background(), "firebase-uid-1", "TestUser")

	require.NoError(t, err)
	assert.NotEmpty(t, player.PlayerID)
	assert.Equal(t, "firebase-uid-1", player.FirebaseUID)
	assert.Equal(t, "TestUser", player.Username)
	assert.Equal(t, int64(1), player.Level)
	assert.Equal(t, int64(0), player.Exp)
	assert.False(t, player.IsPremium)
}

func TestRegister_ThenLoginSucceeds(t *testing.T) {
	svc, _ := newAuthTestService()

	registered, err := svc.Register(context.Background(), "firebase-uid-login", "LoginUser")
	require.NoError(t, err)

	loggedIn, err := svc.Login(context.Background(), "firebase-uid-login")
	require.NoError(t, err)
	assert.Equal(t, registered.PlayerID, loggedIn.PlayerID)
	assert.Equal(t, "LoginUser", loggedIn.Username)
}

func TestRegister_GrantsStarterStamps(t *testing.T) {
	svc, shopRepo := newAuthTestService()
	ctx := context.Background()

	player, err := svc.Register(ctx, "firebase-uid-stamps", "StampUser")
	require.NoError(t, err)

	for i := int64(1); i <= 7; i++ {
		has, err := shopRepo.HasPlayerItem(ctx, player.PlayerID, model.ItemTypeStamp, i)
		require.NoError(t, err)
		assert.True(t, has, "player should own starter stamp %d", i)
	}
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	svc, _ := newAuthTestService()

	_, err := svc.Register(context.Background(), "firebase-uid-dup", "FirstUser")
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), "firebase-uid-dup", "SecondUser")
	require.ErrorIs(t, err, ErrPlayerAlreadyRegistered)
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

// failingStampShopRepo wraps MockShopRepository but returns an error from InsertPlayerItems.
type failingStampShopRepo struct {
	*repository.MockShopRepository
}

func (r *failingStampShopRepo) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	return fmt.Errorf("database connection lost")
}

func TestLogin_PlayerNotFound(t *testing.T) {
	svc, _ := newAuthTestService()

	_, err := svc.Login(context.Background(), "nonexistent-uid")
	require.ErrorIs(t, err, ErrPlayerNotFound)
}
