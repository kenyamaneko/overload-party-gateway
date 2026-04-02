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

// Test config values for exp tests. Centralised so that changing game balance
// only requires updating these constants, not every assertion.
const (
	testExpCoeff = 60
	testExpWin   = 40
	testExpLoss  = 20
	testExpDraw  = 30
)

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
			"exp_formula_coefficient":     testExpCoeff,
			"exp_win":                     testExpWin,
			"exp_loss":                    testExpLoss,
			"exp_draw":                    testExpDraw,
		},
	}
	return NewPlayerService(playerRepo, configRepo), playerRepo
}

func TestAwardExp(t *testing.T) {
	// Level 1→2 requires exp >= coeff * 2^2 = 240
	levelUpThreshold := testExpCoeff * 2 * 2

	tests := []struct {
		name      string
		initExp   int64
		initLevel int64
		gain      int64
		wantExp   int64
		wantLevel int64
	}{
		{"below threshold", 0, 1, testExpWin, testExpWin, 1},
		{"exact level up", int64(levelUpThreshold) - testExpWin, 1, testExpWin, int64(levelUpThreshold), 2},
		{"multiple level ups", 0, 1, int64(testExpCoeff * 4 * 4), int64(testExpCoeff * 4 * 4), 4},
		{"zero gain is noop", 100, 1, 0, 100, 1},
		{"negative gain is noop", 100, 1, -10, 100, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: tt.initLevel, Exp: tt.initExp}
			svc, repo := setupExpService(t, p)

			require.NoError(t, svc.AwardExp(context.Background(), "p1", tt.gain))

			got, _ := repo.FindByID(context.Background(), "p1")
			assert.Equal(t, tt.wantExp, got.Exp)
			assert.Equal(t, tt.wantLevel, got.Level)
		})
	}
}

func TestAwardExp_MissingCoefficient_ReturnsError(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	require.NoError(t, playerRepo.Create(context.Background(), &model.Player{PlayerID: "p1", FirebaseUID: "uid1"}, &model.PlayerDailyBattle{PlayerID: "p1", LastResetDate: today()}))
	configRepo := &mockGameConfigRepo{values: map[string]int64{}}
	svc := NewPlayerService(playerRepo, configRepo)

	err := svc.AwardExp(context.Background(), "p1", testExpWin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exp_formula_coefficient")
}

func TestAwardGameExp(t *testing.T) {
	tests := []struct {
		name      string
		winnerNum int64
		reason    string
		matchType string
		wantP1Exp int64
		wantP2Exp int64
	}{
		{"player1 wins", 1, "system_down", "pvp", testExpWin, testExpLoss},
		{"player2 wins", 2, "budget_zero", "pvp", testExpLoss, testExpWin},
		{"draw", 0, "draw", "pvp", testExpDraw, testExpDraw},
		{"npc match p1 wins", 1, "system_down", "npc", testExpWin, 0},   // wantP2Exp: not asserted (NPC has no P2)
		{"npc match p1 loses", 2, "system_down", "npc", testExpLoss, 0}, // wantP2Exp: not asserted (NPC has no P2)
		{"npc match draw", 0, "draw", "npc", testExpDraw, 0},           // wantP2Exp: not asserted (NPC has no P2)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p1 := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Level: 1, Exp: 0}
			players := []*model.Player{p1}
			if tt.matchType != "npc" {
				players = append(players, &model.Player{PlayerID: "p2", FirebaseUID: "uid2", Level: 1, Exp: 0})
			}
			svc, repo := setupExpService(t, players...)

			p2ID := "p2"
			if tt.matchType == "npc" {
				p2ID = "npc-easy"
			}
			require.NoError(t, svc.AwardGameExp(context.Background(), "p1", p2ID, tt.winnerNum, tt.reason, tt.matchType))

			got1, _ := repo.FindByID(context.Background(), "p1")
			assert.Equal(t, tt.wantP1Exp, got1.Exp)

			if tt.matchType != "npc" {
				got2, _ := repo.FindByID(context.Background(), "p2")
				assert.Equal(t, tt.wantP2Exp, got2.Exp)
			}
		})
	}
}

// --- ComputeLevel tests ---

func TestComputeLevel(t *testing.T) {
	// threshold returns the exp needed to reach the given level.
	// Formula: coeff * level^2
	threshold := func(level int64) int64 {
		return testExpCoeff * level * level
	}

	tests := []struct {
		name         string
		newExp       int64
		currentLevel int64
		wantLevel    int64
	}{
		{"below threshold", threshold(2) - 1, 1, 1},
		{"exact threshold", threshold(2), 1, 2},
		{"multiple level ups", threshold(4), 1, 4},
		{"stays at current", threshold(4) - 1, 3, 3},
		{"level 0 corrected to 1", 0, 0, 1},
		{"never decreases", 0, 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeLevel(tt.newExp, tt.currentLevel, testExpCoeff)
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
