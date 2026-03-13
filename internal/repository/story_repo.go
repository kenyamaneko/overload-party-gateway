package repository

import (
	"context"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// StoryRepo defines the data access contract for story scenario operations.
type StoryRepo interface {
	ListActiveEpisodes(ctx context.Context) ([]*model.ScenarioEpisode, error)
	FindEpisodeByID(ctx context.Context, episodeID string) (*model.ScenarioEpisode, error)
	GetCompletedEpisodeIDs(ctx context.Context, playerID string) ([]string, error)
	MarkComplete(ctx context.Context, playerID, episodeID string) error
}
