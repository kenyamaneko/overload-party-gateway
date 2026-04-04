package service

import (
	"context"
	"fmt"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

type UserSettingsService struct {
	repo port.UserSettingsRepo
}

func NewUserSettingsService(repo port.UserSettingsRepo) *UserSettingsService {
	return &UserSettingsService{repo: repo}
}

func (s *UserSettingsService) Get(ctx context.Context, playerID string) (*model.UserSettings, error) {
	settings, err := s.repo.Get(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get user settings: %w", err)
	}
	if settings == nil {
		settings = &model.UserSettings{
			PlayerID:    playerID,
			Language:    DefaultLanguage,
			BgmVolume:   DefaultBgmVolume,
			SeVolume:    DefaultSeVolume,
			PushEnabled: DefaultPushEnabled,
			UpdatedAt:   time.Now(),
		}
	}
	return settings, nil
}

func (s *UserSettingsService) Update(ctx context.Context, settings *model.UserSettings) error {
	return s.repo.Upsert(ctx, settings)
}
