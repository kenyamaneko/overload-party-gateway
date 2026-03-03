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
	configKeyFreeDailyBattleLimit    = "free_daily_battle_limit"
	configKeyPremiumDailyBattleLimit = "premium_daily_battle_limit"
	defaultFreeDailyBattleLimit      = int64(10)
	defaultPremiumDailyBattleLimit   = int64(30)
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

	db, err := s.playerRepo.GetDailyBattle(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get daily battle: %w", err)
	}

	today := gameDay()
	count := db.DailyBattleCount
	if db.LastResetDate != today {
		count = 0 // Will be reset on next increment
	}

	if player.IsPremium {
		premiumLimit, err := s.gameConfigRepo.GetInt64(ctx, configKeyPremiumDailyBattleLimit, defaultPremiumDailyBattleLimit)
		if err != nil {
			return nil, fmt.Errorf("get premium battle limit: %w", err)
		}
		return &BattleLimitResponse{
			DailyBattleCount: count,
			DailyBattleLimit: premiumLimit,
			CanBattle:        count < premiumLimit,
		}, nil
	}

	freeLimit, err := s.gameConfigRepo.GetInt64(ctx, configKeyFreeDailyBattleLimit, defaultFreeDailyBattleLimit)
	if err != nil {
		return nil, fmt.Errorf("get free battle limit: %w", err)
	}

	return &BattleLimitResponse{
		DailyBattleCount: count,
		DailyBattleLimit: freeLimit,
		CanBattle:        count < freeLimit,
	}, nil
}

// CheckAndIncrementBattleCount verifies the player can battle and increments the count.
// Returns an error if the daily limit has been reached.
func (s *PlayerService) CheckAndIncrementBattleCount(ctx context.Context, playerID string) error {
	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return fmt.Errorf("find player: %w", err)
	}

	today := gameDay()
	newCount, err := s.playerRepo.IncrementDailyBattle(ctx, playerID, today)
	if err != nil {
		return fmt.Errorf("increment daily battle: %w", err)
	}

	if player.IsPremium {
		premiumLimit, err := s.gameConfigRepo.GetInt64(ctx, configKeyPremiumDailyBattleLimit, defaultPremiumDailyBattleLimit)
		if err != nil {
			return fmt.Errorf("get premium battle limit: %w", err)
		}
		if newCount > premiumLimit {
			return fmt.Errorf("daily battle limit reached (%d/%d)", newCount, premiumLimit)
		}
		return nil
	}

	freeLimit, err := s.gameConfigRepo.GetInt64(ctx, configKeyFreeDailyBattleLimit, defaultFreeDailyBattleLimit)
	if err != nil {
		return fmt.Errorf("get free battle limit: %w", err)
	}
	if newCount > freeLimit {
		return fmt.Errorf("daily battle limit reached (%d/%d)", freeLimit, freeLimit)
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
