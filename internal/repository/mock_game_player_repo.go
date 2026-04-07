package repository

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.GamePlayerRepo = (*MockGamePlayerRepository)(nil)

type gamePlayerEntry struct {
	PlayerNum int
	PlayerID  string
}

type MockGamePlayerRepository struct {
	mu      sync.Mutex
	entries map[string][]gamePlayerEntry // gameID → entries
}

func NewMockGamePlayerRepository() *MockGamePlayerRepository {
	return &MockGamePlayerRepository{
		entries: make(map[string][]gamePlayerEntry),
	}
}

func (r *MockGamePlayerRepository) InsertGamePlayer(_ context.Context, gameID string, playerNum int, playerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries[gameID] {
		if e.PlayerNum == playerNum {
			return nil // ON CONFLICT DO NOTHING
		}
	}
	r.entries[gameID] = append(r.entries[gameID], gamePlayerEntry{PlayerNum: playerNum, PlayerID: playerID})
	return nil
}
