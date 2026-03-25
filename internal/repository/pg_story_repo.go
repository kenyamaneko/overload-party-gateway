package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.StoryRepo = (*PgStoryRepository)(nil)

type PgStoryRepository struct {
	pool *pgxpool.Pool
}

func NewPgStoryRepository(pool *pgxpool.Pool) *PgStoryRepository {
	return &PgStoryRepository{pool: pool}
}

func (r *PgStoryRepository) ListActiveEpisodes(ctx context.Context) ([]*model.ScenarioEpisode, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT episode_id, faction, episode_number, title_ja, title_en,
		        required_level, required_factions, required_episodes,
		        script_path, thumbnail_path, sort_order, is_active, created_at
		 FROM scenario_episodes
		 WHERE is_active = true
		 ORDER BY sort_order`)
	if err != nil {
		return nil, fmt.Errorf("query episodes: %w", err)
	}
	defer rows.Close()

	var episodes []*model.ScenarioEpisode
	for rows.Next() {
		var ep model.ScenarioEpisode
		if err := rows.Scan(
			&ep.EpisodeID, &ep.Faction, &ep.EpisodeNumber, &ep.TitleJa, &ep.TitleEn,
			&ep.RequiredLevel, &ep.RequiredFactions, &ep.RequiredEpisodes,
			&ep.ScriptPath, &ep.ThumbnailPath, &ep.SortOrder, &ep.IsActive, &ep.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		episodes = append(episodes, &ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate episodes: %w", err)
	}
	return episodes, nil
}

func (r *PgStoryRepository) FindEpisodeByID(ctx context.Context, episodeID string) (*model.ScenarioEpisode, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT episode_id, faction, episode_number, title_ja, title_en,
		        required_level, required_factions, required_episodes,
		        script_path, thumbnail_path, sort_order, is_active, created_at
		 FROM scenario_episodes
		 WHERE episode_id = $1`,
		episodeID)

	var ep model.ScenarioEpisode
	err := row.Scan(
		&ep.EpisodeID, &ep.Faction, &ep.EpisodeNumber, &ep.TitleJa, &ep.TitleEn,
		&ep.RequiredLevel, &ep.RequiredFactions, &ep.RequiredEpisodes,
		&ep.ScriptPath, &ep.ThumbnailPath, &ep.SortOrder, &ep.IsActive, &ep.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query episode by id: %w", err)
	}
	return &ep, nil
}

func (r *PgStoryRepository) GetCompletedEpisodeIDs(ctx context.Context, playerID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT episode_id FROM player_story_progress WHERE player_id = $1`,
		playerID)
	if err != nil {
		return nil, fmt.Errorf("query completed episodes: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan episode id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completed episodes: %w", err)
	}
	return ids, nil
}

func (r *PgStoryRepository) GetUnlockContext(ctx context.Context, playerID string) (*model.StoryUnlockContext, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT
		   p.level,
		   COALESCE(ARRAY(SELECT faction FROM player_factions WHERE player_id = $1), '{}'),
		   COALESCE(ARRAY(SELECT episode_id FROM player_story_progress WHERE player_id = $1), '{}')
		 FROM players p
		 WHERE p.player_id = $1`,
		playerID)

	var level int64
	var factions []string
	var episodes []string
	if err := row.Scan(&level, &factions, &episodes); err != nil {
		return nil, fmt.Errorf("query unlock context: %w", err)
	}

	factionSet := make(map[string]bool, len(factions))
	for _, f := range factions {
		factionSet[f] = true
	}
	episodeSet := make(map[string]bool, len(episodes))
	for _, e := range episodes {
		episodeSet[e] = true
	}

	return &model.StoryUnlockContext{
		PlayerLevel:       level,
		OwnedFactions:     factionSet,
		CompletedEpisodes: episodeSet,
	}, nil
}

func (r *PgStoryRepository) MarkComplete(ctx context.Context, playerID, episodeID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO player_story_progress (player_id, episode_id)
		 VALUES ($1, $2)
		 ON CONFLICT (player_id, episode_id) DO NOTHING`,
		playerID, episodeID,
	)
	if err != nil {
		return fmt.Errorf("mark episode complete: %w", err)
	}
	return nil
}
