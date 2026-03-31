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

func TestGetPlayer_NotFound_ReturnsNil(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1"}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()}
	svc := setupPlayerService(t, player, daily)

	p, err := svc.GetPlayer(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, p)
}

// --- AwardExp / AwardGameExp tests ---

// setupExpService creates a PlayerService pre-configured for exp tests.
func setupExpService(t *testing.T, players ...*model.Player) (*PlayerService, *repository.MockPlayerRepository) {
	t.Helper()
	playerRepo := repository.NewMockPlayerRepository()
	for _, p := range players {
		require.NoError(t, playerRepo.Create(context.Background(), p, &model.PlayerDailyBattle{
			PlayerID:         p.PlayerID,
			DailyBattleCount: 0,
			LastResetDate:    today(),
		}))
	}
	configRepo := &mockGameConfigRepo{
		values: map[string]int64{
			configKeyFreeDailyBattleLimit: 10,
			"exp_formula_coefficient":     60,
			"exp_win":                     40,
			"exp_loss":                    20,
			"exp_draw":                    30,
		},
	}
	return NewPlayerService(playerRepo, configRepo), playerRepo
}

func TestAwardExp_AddsExpAndLevel(t *testing.T) {
	p := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p)
	ctx := context.Background()

	require.NoError(t, svc.AwardExp(ctx, "p1", 40))

	got, _ := repo.FindByID(ctx, "p1")
	assert.Equal(t, int64(40), got.Exp)
	assert.Equal(t, int64(1), got.Level) // 60*2*2=240 needed for level 2
}

func TestAwardExp_LevelUp(t *testing.T) {
	// Level 1 → 2 requires exp >= 60*2*2 = 240
	p := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 200}
	svc, repo := setupExpService(t, p)
	ctx := context.Background()

	require.NoError(t, svc.AwardExp(ctx, "p1", 40))

	got, _ := repo.FindByID(ctx, "p1")
	assert.Equal(t, int64(240), got.Exp)
	assert.Equal(t, int64(2), got.Level)
}

func TestAwardExp_MultipleLevelUps(t *testing.T) {
	// Level 1, exp=0, gain enough to reach level 3 (60*4*4=960)
	p := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p)
	ctx := context.Background()

	require.NoError(t, svc.AwardExp(ctx, "p1", 960))

	got, _ := repo.FindByID(ctx, "p1")
	assert.Equal(t, int64(960), got.Exp)
	assert.Equal(t, int64(4), got.Level) // 960 >= 60*4*4=960 → level 4
}

func TestAwardExp_ZeroOrNegative_Noop(t *testing.T) {
	p := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 100}
	svc, repo := setupExpService(t, p)
	ctx := context.Background()

	require.NoError(t, svc.AwardExp(ctx, "p1", 0))
	require.NoError(t, svc.AwardExp(ctx, "p1", -10))

	got, _ := repo.FindByID(ctx, "p1")
	assert.Equal(t, int64(100), got.Exp)
}

func TestAwardExp_MissingCoefficient_ReturnsError(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	require.NoError(t, playerRepo.Create(context.Background(), &model.Player{PlayerID: "p1", FirebaseUID: "uid1"}, &model.PlayerDailyBattle{PlayerID: "p1", LastResetDate: today()}))
	configRepo := &mockGameConfigRepo{values: map[string]int64{}} // no exp_formula_coefficient
	svc := NewPlayerService(playerRepo, configRepo)

	err := svc.AwardExp(context.Background(), "p1", 40)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exp_formula_coefficient")
}

func TestAwardGameExp_Player1Wins(t *testing.T) {
	p1 := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
	p2 := &model.Player{PlayerID: "p2", FirebaseUID: "uid2", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p1, p2)
	ctx := context.Background()

	require.NoError(t, svc.AwardGameExp(ctx, "p1", "p2", 1, "system_down"))

	got1, _ := repo.FindByID(ctx, "p1")
	got2, _ := repo.FindByID(ctx, "p2")
	assert.Equal(t, int64(40), got1.Exp) // win
	assert.Equal(t, int64(20), got2.Exp) // loss
}

func TestAwardGameExp_Player2Wins(t *testing.T) {
	p1 := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
	p2 := &model.Player{PlayerID: "p2", FirebaseUID: "uid2", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p1, p2)
	ctx := context.Background()

	require.NoError(t, svc.AwardGameExp(ctx, "p1", "p2", 2, "budget_zero"))

	got1, _ := repo.FindByID(ctx, "p1")
	got2, _ := repo.FindByID(ctx, "p2")
	assert.Equal(t, int64(20), got1.Exp) // loss
	assert.Equal(t, int64(40), got2.Exp) // win
}

func TestAwardGameExp_Draw(t *testing.T) {
	p1 := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
	p2 := &model.Player{PlayerID: "p2", FirebaseUID: "uid2", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p1, p2)
	ctx := context.Background()

	require.NoError(t, svc.AwardGameExp(ctx, "p1", "p2", 0, "draw"))

	got1, _ := repo.FindByID(ctx, "p1")
	got2, _ := repo.FindByID(ctx, "p2")
	assert.Equal(t, int64(30), got1.Exp)
	assert.Equal(t, int64(30), got2.Exp)
}

func TestAwardGameExp_NpcSkipped(t *testing.T) {
	p1 := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p1)
	ctx := context.Background()

	// NPC player doesn't exist in repo — should not error because it's skipped.
	require.NoError(t, svc.AwardGameExp(ctx, "p1", "npc-easy", 1, "system_down"))

	got1, _ := repo.FindByID(ctx, "p1")
	assert.Equal(t, int64(40), got1.Exp) // winner
}

func TestAwardGameExp_NpcIsPlayer1(t *testing.T) {
	p2 := &model.Player{PlayerID: "p2", FirebaseUID: "uid2", Level: 1, Exp: 0}
	svc, repo := setupExpService(t, p2)
	ctx := context.Background()

	require.NoError(t, svc.AwardGameExp(ctx, "npc-hard", "p2", 2, "system_down"))

	got2, _ := repo.FindByID(ctx, "p2")
	assert.Equal(t, int64(40), got2.Exp) // winner
}

// --- ComputeLevel tests ---

func TestComputeLevel(t *testing.T) {
	tests := []struct {
		name         string
		newExp       int64
		currentLevel int64
		coeff        int64
		wantLevel    int64
	}{
		{"no level up", 100, 1, 60, 1},                 // next=60*4=240
		{"exact threshold", 240, 1, 60, 2},              // 240 >= 60*4=240 → level 2
		{"just below", 239, 1, 60, 1},                   // 239 < 240
		{"multiple level ups", 960, 1, 60, 4},            // 960 >= 60*4=240, >= 60*9=540, >= 60*16=960 → level 4
		{"stays at current", 500, 3, 60, 3},              // next=60*16=960
		{"level 0 corrected to 1", 0, 0, 60, 1},         // minimum level
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repository.ComputeLevel(tt.newExp, tt.currentLevel, tt.coeff)
			assert.Equal(t, tt.wantLevel, got)
		})
	}
}

func TestNotFound(t *testing.T) {
	tests := []struct {
		name string
		fn   func(svc *PlayerService) error
	}{
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
