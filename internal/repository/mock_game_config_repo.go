package repository

import (
	"context"
	"fmt"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// MockGameConfigRepository はテスト用の GameConfigRepo インメモリ実装です。
type MockGameConfigRepository struct {
	values map[string]int64
}

func NewMockGameConfigRepository() *MockGameConfigRepository {
	return &MockGameConfigRepository{
		values: map[string]int64{
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

// GetInt64 は指定キーの設定値を返します。キーが存在しなければ ErrNotFound を返します。
func (m *MockGameConfigRepository) GetInt64(_ context.Context, key string) (int64, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return 0, fmt.Errorf("game config %q: %w", key, port.ErrNotFound)
}

func (m *MockGameConfigRepository) SetForTest(key string, value int64) {
	m.values[key] = value
}
