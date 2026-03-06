package repository

import "context"

// MockGameConfigRepository is an in-memory mock for GameConfigRepo.
type MockGameConfigRepository struct {
	values map[string]int64
}

func NewMockGameConfigRepository() *MockGameConfigRepository {
	return &MockGameConfigRepository{
		values: map[string]int64{
			"free_daily_battle_limit": 10,
		},
	}
}

func (m *MockGameConfigRepository) GetInt64(_ context.Context, key string, fallback int64) (int64, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return fallback, nil
}

// SetForTest allows tests to override config values.
func (m *MockGameConfigRepository) SetForTest(key string, value int64) {
	m.values[key] = value
}
