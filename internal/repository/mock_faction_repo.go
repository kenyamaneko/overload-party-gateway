package repository

import (
	"context"
	"sync"
)

var _ FactionRepo = (*MockFactionRepository)(nil)

type MockFactionRepository struct {
	mu       sync.Mutex
	factions map[string]map[string]string // playerID -> faction -> source
}

func NewMockFactionRepository() *MockFactionRepository {
	return &MockFactionRepository{
		factions: make(map[string]map[string]string),
	}
}

func (r *MockFactionRepository) AddPlayerFaction(_ context.Context, playerID, faction, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.factions[playerID] == nil {
		r.factions[playerID] = make(map[string]string)
	}
	if _, exists := r.factions[playerID][faction]; !exists {
		r.factions[playerID][faction] = source
	}
	return nil
}

func (r *MockFactionRepository) GetPlayerFactions(_ context.Context, playerID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []string
	for f := range r.factions[playerID] {
		result = append(result, f)
	}
	return result, nil
}
