package repository

import (
	"context"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-common/model"
)

// MockUserSettingsRepository is an in-memory implementation of UserSettingsRepo for local mode.
type MockUserSettingsRepository struct {
	mu       sync.Mutex
	settings map[string]*model.UserSettings // playerID → UserSettings
}

var _ UserSettingsRepo = (*MockUserSettingsRepository)(nil)

func NewMockUserSettingsRepository() *MockUserSettingsRepository {
	return &MockUserSettingsRepository{
		settings: make(map[string]*model.UserSettings),
	}
}

func (r *MockUserSettingsRepository) Get(_ context.Context, playerID string) (*model.UserSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.settings[playerID]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (r *MockUserSettingsRepository) Upsert(_ context.Context, s *model.UserSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s.UpdatedAt = time.Now()
	r.settings[s.PlayerID] = s
	return nil
}
