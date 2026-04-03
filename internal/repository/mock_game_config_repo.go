package repository

import "context"

// MockGameConfigRepository is an in-memory mock for GameConfigRepo.
type MockGameConfigRepository struct {
	values map[string]int64
}

func NewMockGameConfigRepository() *MockGameConfigRepository {
	return &MockGameConfigRepository{
		values: map[string]int64{
			// Keep in sync with overload-party-common/db/seed/game_config.sql
			"free_daily_battle_limit":    10,
			"premium_daily_battle_limit": 30,
			"initial_time_bank":          480,
			"exp_win":                    40,
			"exp_loss":                   20,
			"exp_draw":                   30,
			"exp_formula_coefficient":    60,
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
