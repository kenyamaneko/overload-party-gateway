package repository

import (
	"context"
	"fmt"
	"sync"

	"cloud.google.com/go/civil"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// MockPlayerRepository is an in-memory implementation of PlayerRepo for local mode and testing.
type MockPlayerRepository struct {
	mu           sync.Mutex
	players      map[string]*model.Player // playerID → Player
	dailyBattles map[string]*model.PlayerDailyBattle
	byUID        map[string]string // firebaseUID → playerID
}

var _ PlayerRepo = (*MockPlayerRepository)(nil)

func NewMockPlayerRepository() *MockPlayerRepository {
	return &MockPlayerRepository{
		players:      make(map[string]*model.Player),
		dailyBattles: make(map[string]*model.PlayerDailyBattle),
		byUID:        make(map[string]string),
	}
}

func (r *MockPlayerRepository) Create(ctx context.Context, player *model.Player, dailyBattle *model.PlayerDailyBattle) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byUID[player.FirebaseUID]; ok {
		return fmt.Errorf("player with firebase_uid %s already exists", player.FirebaseUID)
	}

	r.players[player.PlayerID] = player
	r.dailyBattles[player.PlayerID] = dailyBattle
	r.byUID[player.FirebaseUID] = player.PlayerID
	return nil
}

func (r *MockPlayerRepository) FindByID(ctx context.Context, playerID string) (*model.Player, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.players[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s not found", playerID)
	}
	return p, nil
}

func (r *MockPlayerRepository) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*model.Player, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pid, ok := r.byUID[firebaseUID]
	if !ok {
		return nil, nil // Not found is not an error
	}
	return r.players[pid], nil
}

func (r *MockPlayerRepository) GetDailyBattle(ctx context.Context, playerID string) (*model.PlayerDailyBattle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	db, ok := r.dailyBattles[playerID]
	if !ok {
		return nil, fmt.Errorf("daily battle data for player %s not found", playerID)
	}
	return db, nil
}

func (r *MockPlayerRepository) IncrementDailyBattle(ctx context.Context, playerID string, today civil.Date) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	db, ok := r.dailyBattles[playerID]
	if !ok {
		return 0, fmt.Errorf("daily battle data for player %s not found", playerID)
	}

	if db.LastResetDate != today {
		db.DailyBattleCount = 1
		db.LastResetDate = today
	} else {
		db.DailyBattleCount++
	}
	return db.DailyBattleCount, nil
}

func (r *MockPlayerRepository) UpdateUsername(ctx context.Context, playerID string, username string) (*model.Player, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.players[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s not found", playerID)
	}
	p.Username = username
	return p, nil
}
