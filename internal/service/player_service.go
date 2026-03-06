package service

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/civil"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const (
	configKeyFreeDailyBattleLimit = "free_daily_battle_limit"
)

type PlayerService struct {
	playerRepo     repository.PlayerRepo
	gameConfigRepo repository.GameConfigRepo
}

func NewPlayerService(playerRepo repository.PlayerRepo, gameConfigRepo repository.GameConfigRepo) *PlayerService {
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

// gameDay returns the current "game day" date.
// The game day resets at JST 05:00 (= UTC 20:00).
// Adding 4 hours (JST+9 minus 5h offset) to UTC and taking the date achieves this.
func gameDay() civil.Date {
	now := time.Now().UTC().Add(4 * time.Hour)
	return civil.DateOf(now)
}
