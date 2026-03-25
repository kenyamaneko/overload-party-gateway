package repository

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.StoryRepo = (*MockStoryRepository)(nil)

type MockStoryRepository struct {
	mu        sync.Mutex
	episodes  []*model.ScenarioEpisode
	completed map[string]map[string]bool // playerID -> episodeID -> true

	// External repos used by GetUnlockContext to mirror the PG combined query.
	playerRepo  *MockPlayerRepository
	factionRepo *MockFactionRepository
}

func NewMockStoryRepository() *MockStoryRepository {
	return &MockStoryRepository{
		completed: make(map[string]map[string]bool),
	}
}

// SetDeps wires up related mock repos so GetUnlockContext can work.
func (r *MockStoryRepository) SetDeps(playerRepo *MockPlayerRepository, factionRepo *MockFactionRepository) {
	r.playerRepo = playerRepo
	r.factionRepo = factionRepo
}

func (r *MockStoryRepository) SeedEpisodes(episodes []*model.ScenarioEpisode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.episodes = episodes
}

func (r *MockStoryRepository) ListActiveEpisodes(_ context.Context) ([]*model.ScenarioEpisode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*model.ScenarioEpisode
	for _, ep := range r.episodes {
		if ep.IsActive {
			result = append(result, ep)
		}
	}
	return result, nil
}

func (r *MockStoryRepository) FindEpisodeByID(_ context.Context, episodeID string) (*model.ScenarioEpisode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ep := range r.episodes {
		if ep.EpisodeID == episodeID {
			return ep, nil
		}
	}
	return nil, nil
}

func (r *MockStoryRepository) GetCompletedEpisodeIDs(_ context.Context, playerID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for id := range r.completed[playerID] {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *MockStoryRepository) GetUnlockContext(ctx context.Context, playerID string) (*model.StoryUnlockContext, error) {
	// Player level
	var level int64
	if r.playerRepo != nil {
		p, err := r.playerRepo.FindByID(ctx, playerID)
		if err != nil {
			return nil, err
		}
		if p != nil {
			level = p.Level
		}
	}

	// Owned factions
	factionSet := make(map[string]bool)
	if r.factionRepo != nil {
		factions, err := r.factionRepo.GetPlayerFactions(ctx, playerID)
		if err != nil {
			return nil, err
		}
		for _, f := range factions {
			factionSet[f] = true
		}
	}

	// Completed episodes
	r.mu.Lock()
	completedSet := make(map[string]bool, len(r.completed[playerID]))
	for id := range r.completed[playerID] {
		completedSet[id] = true
	}
	r.mu.Unlock()

	return &model.StoryUnlockContext{
		PlayerLevel:       level,
		OwnedFactions:     factionSet,
		CompletedEpisodes: completedSet,
	}, nil
}

func (r *MockStoryRepository) MarkComplete(_ context.Context, playerID, episodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.completed[playerID] == nil {
		r.completed[playerID] = make(map[string]bool)
	}
	r.completed[playerID][episodeID] = true
	return nil
}
