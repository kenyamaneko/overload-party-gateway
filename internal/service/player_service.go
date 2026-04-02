package service

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/civil"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

const (
	configKeyFreeDailyBattleLimit = "free_daily_battle_limit"

	// gameDayOffset is the UTC offset used to calculate the "game day".
	// The game day resets at JST 05:00 (= UTC 20:00 of the previous calendar day).
	// Offset derivation: JST (UTC+9) minus 5 h reset = +4 h from UTC.
	// Example: JST 2024-01-02 04:59 → UTC 2024-01-01 19:59 + 4h = Jan 2 (game day 2024-01-01)
	//          JST 2024-01-02 05:00 → UTC 2024-01-01 20:00 + 4h = Jan 2 (game day 2024-01-02)
	gameDayOffset = 4 * time.Hour
)

type PlayerService struct {
	playerRepo     port.PlayerRepo
	gameConfigRepo port.GameConfigRepo
}

func NewPlayerService(playerRepo port.PlayerRepo, gameConfigRepo port.GameConfigRepo) *PlayerService {
	return &PlayerService{playerRepo: playerRepo, gameConfigRepo: gameConfigRepo}
}

func (s *PlayerService) UpdateUsername(ctx context.Context, playerID string, name string) (*model.Player, error) {
	return s.playerRepo.UpdateUsername(ctx, playerID, name)
}

func (s *PlayerService) GetPlayer(ctx context.Context, playerID string) (*model.Player, error) {
	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("find player: %w", err)
	}
	return player, nil
}

type BattleLimitResponse struct {
	DailyBattleCount int64 `json:"daily_battle_count"`
	DailyBattleLimit int64 `json:"daily_battle_limit"` // -1 = unlimited
	CanBattle        bool  `json:"can_battle"`
}

func (s *PlayerService) GetBattleLimit(ctx context.Context, playerID string) (*BattleLimitResponse, error) {
	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("find player: %w", err)
	}
	if player == nil {
		return nil, fmt.Errorf("player %s not found", playerID)
	}

	if player.IsPremium {
		return &BattleLimitResponse{
			DailyBattleCount: 0,
			DailyBattleLimit: -1,
			CanBattle:        true,
		}, nil
	}

	db, err := s.playerRepo.GetDailyBattle(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get daily battle: %w", err)
	}

	today := gameDay()
	count := db.DailyBattleCount
	if db.LastResetDate != today {
		count = 0 // Will be reset on next increment
	}

	freeLimit, err := s.gameConfigRepo.GetInt64(ctx, configKeyFreeDailyBattleLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("get free battle limit: %w", err)
	}
	if freeLimit == 0 {
		return nil, fmt.Errorf("game config %q is not set", configKeyFreeDailyBattleLimit)
	}

	return &BattleLimitResponse{
		DailyBattleCount: count,
		DailyBattleLimit: freeLimit,
		CanBattle:        count < freeLimit,
	}, nil
}

// IncrementBattleCount increments the daily battle count for a player.
// It also records the increment even for premium players.
func (s *PlayerService) IncrementBattleCount(ctx context.Context, playerID string) error {
	today := gameDay()

	_, err := s.playerRepo.IncrementDailyBattle(ctx, playerID, today)
	if err != nil {
		return fmt.Errorf("increment daily battle: %w", err)
	}

	return nil
}

// AwardExp adds experience points to a player and recalculates their level.
func (s *PlayerService) AwardExp(ctx context.Context, playerID string, expGain int64) error {
	if expGain <= 0 {
		return nil
	}
	coeff, err := s.gameConfigRepo.GetInt64(ctx, "exp_formula_coefficient", 0)
	if err != nil {
		return fmt.Errorf("get exp_formula_coefficient: %w", err)
	}
	if coeff <= 0 {
		return fmt.Errorf("exp_formula_coefficient not configured in game_config")
	}
	_, err = s.playerRepo.AddExp(ctx, playerID, expGain, func(newExp, currentLevel int64) int64 {
		return ComputeLevel(newExp, currentLevel, coeff)
	})
	return err
}

// ComputeLevel determines the new level after gaining exp.
// It only increments from the current level — never decreases — so that
// formula coefficient changes do not retroactively alter existing levels.
func ComputeLevel(newExp, currentLevel, coeff int64) int64 {
	level := currentLevel
	if level < 1 {
		level = 1
	}
	for {
		nextLevelExp := coeff * (level + 1) * (level + 1)
		if newExp < nextLevelExp {
			break
		}
		level++
	}
	return level
}

// AwardGameExp grants experience to both players after a game ends.
// It reads exp_win/exp_loss/exp_draw from game_config and awards accordingly.
// For NPC matches (matchType == "npc"), only the human player (player1) receives exp.
func (s *PlayerService) AwardGameExp(ctx context.Context, player1ID, player2ID string, winnerNum int64, reason, matchType string) error {
	expWin, err := s.gameConfigRepo.GetInt64(ctx, "exp_win", 0)
	if err != nil {
		return fmt.Errorf("read exp_win: %w", err)
	}
	expLoss, err := s.gameConfigRepo.GetInt64(ctx, "exp_loss", 0)
	if err != nil {
		return fmt.Errorf("read exp_loss: %w", err)
	}
	expDraw, err := s.gameConfigRepo.GetInt64(ctx, "exp_draw", 0)
	if err != nil {
		return fmt.Errorf("read exp_draw: %w", err)
	}

	isNpc := matchType == constants.MatchTypeNpc

	award := func(playerID string, exp int64, isNpcSide bool) error {
		if isNpcSide {
			return nil
		}
		return s.AwardExp(ctx, playerID, exp)
	}

	switch {
	case reason == constants.WinReasonDraw || winnerNum == 0:
		if err := award(player1ID, expDraw, false); err != nil {
			return err
		}
		return award(player2ID, expDraw, isNpc)
	case winnerNum == 1:
		if err := award(player1ID, expWin, false); err != nil {
			return err
		}
		return award(player2ID, expLoss, isNpc)
	case winnerNum == 2:
		if err := award(player1ID, expLoss, false); err != nil {
			return err
		}
		return award(player2ID, expWin, isNpc)
	}
	return nil
}

// LevelProgress holds computed level progress fields derived from a player's
// current level, exp, and the exp_formula_coefficient config value.
type LevelProgress struct {
	LevelExpCurrent  int64 `json:"level_exp_current"`
	LevelExpRequired int64 `json:"level_exp_required"`
}

// GetLevelProgress returns the level progress for the given player.
func (s *PlayerService) GetLevelProgress(ctx context.Context, level, exp int64) (*LevelProgress, error) {
	coeff, err := s.gameConfigRepo.GetInt64(ctx, "exp_formula_coefficient", 0)
	if err != nil {
		return nil, fmt.Errorf("get exp_formula_coefficient: %w", err)
	}
	if coeff <= 0 {
		return nil, fmt.Errorf("exp_formula_coefficient not configured in game_config")
	}
	return ComputeLevelProgress(level, exp, coeff), nil
}

// ComputeLevelProgress calculates experience progress within the current level.
func ComputeLevelProgress(level, exp, coeff int64) *LevelProgress {
	currentLevelExp := coeff * level * level
	nextLevelExp := coeff * (level + 1) * (level + 1)
	return &LevelProgress{
		LevelExpCurrent:  max(0, exp-currentLevelExp),
		LevelExpRequired: nextLevelExp - currentLevelExp,
	}
}

func gameDay() civil.Date {
	return civil.DateOf(time.Now().UTC().Add(gameDayOffset))
}
