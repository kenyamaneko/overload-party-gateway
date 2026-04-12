package repository

import (
	"context"
	"fmt"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.GamePlayerRepo = (*MockGamePlayerRepository)(nil)

// MockGamePlayerRepository はテスト用のインメモリ GamePlayerRepo 実装です
type MockGamePlayerRepository struct {
	mu      sync.Mutex
	entries map[string][]mockGamePlayerEntry // gameID → entries
}

type mockGamePlayerEntry struct {
	PlayerNum  int
	PlayerID   string
	ExpAwarded bool
}

// NewMockGamePlayerRepository は MockGamePlayerRepository を生成します
func NewMockGamePlayerRepository() *MockGamePlayerRepository {
	return &MockGamePlayerRepository{
		entries: make(map[string][]mockGamePlayerEntry),
	}
}

// InsertGamePlayer はゲームにプレイヤースロットを登録します
func (r *MockGamePlayerRepository) InsertGamePlayer(_ context.Context, gameID string, playerNum int, playerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries[gameID] {
		if e.PlayerNum == playerNum {
			return nil // ON CONFLICT DO NOTHING
		}
	}
	r.entries[gameID] = append(r.entries[gameID], mockGamePlayerEntry{PlayerNum: playerNum, PlayerID: playerID})
	return nil
}

// LookupPlayerNum はゲーム内のプレイヤー番号を取得します
func (r *MockGamePlayerRepository) LookupPlayerNum(_ context.Context, gameID string, playerID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries[gameID] {
		if e.PlayerID == playerID {
			return e.PlayerNum, nil
		}
	}
	return 0, fmt.Errorf("player %s not found in game %s", playerID, gameID)
}

// LookupGamePlayers はゲームの全プレイヤーエントリを取得します
func (r *MockGamePlayerRepository) LookupGamePlayers(_ context.Context, gameID string) ([]port.GamePlayerEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mockEntries := r.entries[gameID]
	result := make([]port.GamePlayerEntry, len(mockEntries))
	for i, e := range mockEntries {
		result[i] = port.GamePlayerEntry{PlayerNum: e.PlayerNum, PlayerID: e.PlayerID}
	}
	return result, nil
}

// MarkExpAwarded は経験値付与済みフラグを設定します
func (r *MockGamePlayerRepository) MarkExpAwarded(_ context.Context, gameID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.entries[gameID] {
		if e.PlayerNum == 1 && !e.ExpAwarded {
			r.entries[gameID][i].ExpAwarded = true
			return true, nil
		}
	}
	return false, nil
}
