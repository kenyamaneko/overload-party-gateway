package repository

import (
	"context"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
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

func (r *MockUserSettingsRepository) Upsert(ctx context.Context, s *model.UserSettings) error {
	return r.UpsertWithTx(ctx, nil, s)
}

func (r *MockUserSettingsRepository) UpsertWithTx(_ context.Context, _ DBTX, s *model.UserSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s.UpdatedAt = time.Now()
	r.settings[s.PlayerID] = s
	return nil
}
