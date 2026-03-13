package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// mockGameConfigRepo is a simple mock that returns configured values.
type mockGameConfigRepo struct {
	values map[string]int64
}

func (m *mockGameConfigRepo) GetInt64(ctx context.Context, key string, fallback int64) (int64, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return fallback, nil
}

// today returns the current game day (UTC+4h), matching the gameDay() logic in player_service.go.
func today() civil.Date {
	return civil.DateOf(time.Now().UTC().Add(4 * time.Hour))
}

// yesterday returns one day before the current game day.
func yesterday() civil.Date {
	d := today()
	return civil.Date{Year: d.Year, Month: d.Month, Day: d.Day - 1}
}

// setupPlayerService creates a PlayerService with a MockPlayerRepository and mockGameConfigRepo,
// and registers a player with the given daily battle data.
func setupPlayerService(t *testing.T, player *model.Player, dailyBattle *model.PlayerDailyBattle) *PlayerService {
	t.Helper()

	playerRepo := repository.NewMockPlayerRepository()
	require.NoError(t, playerRepo.Create(context.Background(), player, dailyBattle))

	configRepo := &mockGameConfigRepo{
		values: map[string]int64{
			configKeyFreeDailyBattleLimit: 10,
		},
	}

	return NewPlayerService(playerRepo, configRepo)
}

// --- GetBattleLimit tests ---

func TestGetBattleLimit(t *testing.T) {
	tests := []struct {
		name             string
		isPremium        bool
		dailyBattleCount int64
		lastResetDate    civil.Date
		wantCount        int64
		wantLimit        int64
		wantCanBattle    bool
	}{
		{
			name:             "FreePlayer_UnderLimit",
			isPremium:        false,
			dailyBattleCount: 3,
			lastResetDate:    today(),
			wantCount:        3,
			wantLimit:        10,
			wantCanBattle:    true,
		},
		{
			name:             "FreePlayer_AtLimit",
			isPremium:        false,
			dailyBattleCount: 10,
			lastResetDate:    today(),
			wantCount:        10,
			wantLimit:        10,
			wantCanBattle:    false,
		},
		{
			name:             "PremiumPlayer",
			isPremium:        true,
			dailyBattleCount: 5,
			lastResetDate:    today(),
			wantCount:        0,
			wantLimit:        -1,
			wantCanBattle:    true,
		},
		{
			name:             "DateReset",
			isPremium:        false,
			dailyBattleCount: 7,
			lastResetDate:    yesterday(),
			wantCount:        0,
			wantLimit:        10,
			wantCanBattle:    true,
		},
		{
			name:             "FreePlayer_OverLimit",
			isPremium:        false,
			dailyBattleCount: 11,
			lastResetDate:    today(),
			wantCount:        11,
			wantLimit:        10,
			wantCanBattle:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: tt.isPremium}
			daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: tt.dailyBattleCount, LastResetDate: tt.lastResetDate}

			svc := setupPlayerService(t, player, daily)

			resp, err := svc.GetBattleLimit(context.Background(), "p1")
			require.NoError(t, err)

			assert.Equal(t, tt.wantCount, resp.DailyBattleCount)
			assert.Equal(t, tt.wantLimit, resp.DailyBattleLimit)
			assert.Equal(t, tt.wantCanBattle, resp.CanBattle)
		})
	}
}

// --- IncrementBattleCount tests ---

func TestIncrementBattleCount_Success(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 5, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	require.NoError(t, svc.IncrementBattleCount(context.Background(), "p1"))

	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, int64(6), resp.DailyBattleCount)
}

func TestIncrementBattleCount_PremiumPlayer(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: true}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 29, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	require.NoError(t, svc.IncrementBattleCount(context.Background(), "p1"))

	// プレミアムは無制限なので DailyBattleLimit = -1
	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, int64(-1), resp.DailyBattleLimit)
	assert.Equal(t, int64(0), resp.DailyBattleCount)
}

// --- GetPlayer / UpdateUsername tests ---

func TestGetPlayer_Success(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Username: "Alice"}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	got, err := svc.GetPlayer(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, "p1", got.PlayerID)
	assert.Equal(t, "Alice", got.Username)
}

func TestUpdateUsername_Success(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Username: "Alice"}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	updated, err := svc.UpdateUsername(context.Background(), "p1", "Bob")
	require.NoError(t, err)

	t.Run("returns updated username", func(t *testing.T) {
		assert.Equal(t, "Bob", updated.Username)
	})

	t.Run("persists updated username", func(t *testing.T) {
		got, err := svc.GetPlayer(context.Background(), "p1")
		require.NoError(t, err)
		assert.Equal(t, "Bob", got.Username)
	})
}

// --- Boundary value tests ---

func TestGetBattleLimit_FreeLimitZero_ReturnsError(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	_ = playerRepo.Create(context.Background(), &model.Player{PlayerID: "p1", FirebaseUID: "uid1"}, &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()})
	configRepo := &mockGameConfigRepo{values: map[string]int64{configKeyFreeDailyBattleLimit: 0}}
	svc := NewPlayerService(playerRepo, configRepo)

	_, err := svc.GetBattleLimit(context.Background(), "p1")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "game config"))
}

// --- Error path tests ---

func TestNotFound(t *testing.T) {
	tests := []struct {
		name string
		fn   func(svc *PlayerService) error
	}{
		{
			name: "GetPlayer_NotFound",
			fn: func(svc *PlayerService) error {
				_, err := svc.GetPlayer(context.Background(), "nonexistent")
				return err
			},
		},
		{
			name: "UpdateUsername_NotFound",
			fn: func(svc *PlayerService) error {
				_, err := svc.UpdateUsername(context.Background(), "nonexistent", "Bob")
				return err
			},
		},
		{
			name: "IncrementBattleCount_PlayerNotFound",
			fn: func(svc *PlayerService) error {
				return svc.IncrementBattleCount(context.Background(), "nonexistent")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1"}
			daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()}

			svc := setupPlayerService(t, player, daily)

			err := tt.fn(svc)
			require.Error(t, err)
		})
	}
}
