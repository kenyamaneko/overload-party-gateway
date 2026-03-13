package repository

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

var _ StoryRepo = (*MockStoryRepository)(nil)

type MockStoryRepository struct {
	mu        sync.Mutex
	episodes  []*model.ScenarioEpisode
	completed map[string]map[string]bool // playerID -> episodeID -> true
}

func NewMockStoryRepository() *MockStoryRepository {
	return &MockStoryRepository{
		completed: make(map[string]map[string]bool),
	}
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

func (r *MockStoryRepository) MarkComplete(_ context.Context, playerID, episodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.completed[playerID] == nil {
		r.completed[playerID] = make(map[string]bool)
	}
	r.completed[playerID][episodeID] = true
	return nil
}
