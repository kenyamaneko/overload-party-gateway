package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

var (
	ErrEpisodeNotFound = fmt.Errorf("episode not found")
	ErrEpisodeLocked   = fmt.Errorf("episode is locked")
)

type StoryService struct {
	storyRepo   repository.StoryRepo
	factionRepo repository.FactionRepo
	playerRepo  repository.PlayerRepo
	gcsClient   *storage.Client
	bucketName  string
}

func NewStoryService(
	storyRepo repository.StoryRepo,
	factionRepo repository.FactionRepo,
	playerRepo repository.PlayerRepo,
	gcsClient *storage.Client,
	bucketName string,
) *StoryService {
	return &StoryService{
		storyRepo:   storyRepo,
		factionRepo: factionRepo,
		playerRepo:  playerRepo,
		gcsClient:   gcsClient,
		bucketName:  bucketName,
	}
}

func (s *StoryService) ListEpisodes(ctx context.Context, playerID, lang string) ([]model.EpisodeWithStatus, error) {
	episodes, err := s.storyRepo.ListActiveEpisodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}

	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("find player: %w", err)
	}

	ownedFactions, err := s.factionRepo.GetPlayerFactions(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get player factions: %w", err)
	}
	factionSet := make(map[string]bool, len(ownedFactions))
	for _, f := range ownedFactions {
		factionSet[f] = true
	}

	completedIDs, err := s.storyRepo.GetCompletedEpisodeIDs(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get completed episodes: %w", err)
	}
	completedSet := make(map[string]bool, len(completedIDs))
	for _, id := range completedIDs {
		completedSet[id] = true
	}

	result := make([]model.EpisodeWithStatus, 0, len(episodes))
	for _, ep := range episodes {
		status := model.EpisodeWithStatus{
			EpisodeID:     ep.EpisodeID,
			Faction:       ep.Faction,
			EpisodeNumber: ep.EpisodeNumber,
			Title:         episodeTitle(ep, lang),
			IsCompleted:   completedSet[ep.EpisodeID],
		}

		lockReason := checkUnlock(ep, player.Level, factionSet, completedSet)
		status.IsUnlocked = lockReason == nil
		status.LockReason = lockReason

		result = append(result, status)
	}
	return result, nil
}

func (s *StoryService) GetScript(ctx context.Context, playerID, episodeID, lang string) (string, error) {
	ep, err := s.storyRepo.FindEpisodeByID(ctx, episodeID)
	if err != nil {
		return "", fmt.Errorf("find episode: %w", err)
	}
	if ep == nil || !ep.IsActive {
		return "", ErrEpisodeNotFound
	}

	if err := s.validateUnlock(ctx, ep, playerID); err != nil {
		return "", err
	}

	script, err := s.readScript(ctx, ep.ScriptPath, lang)
	if err != nil {
		return "", fmt.Errorf("read script: %w", err)
	}
	return script, nil
}

func (s *StoryService) CompleteEpisode(ctx context.Context, playerID, episodeID string) error {
	ep, err := s.storyRepo.FindEpisodeByID(ctx, episodeID)
	if err != nil {
		return fmt.Errorf("find episode: %w", err)
	}
	if ep == nil || !ep.IsActive {
		return ErrEpisodeNotFound
	}

	if err := s.validateUnlock(ctx, ep, playerID); err != nil {
		return err
	}

	if err := s.storyRepo.MarkComplete(ctx, playerID, episodeID); err != nil {
		return fmt.Errorf("mark complete: %w", err)
	}
	return nil
}

func (s *StoryService) validateUnlock(ctx context.Context, ep *model.ScenarioEpisode, playerID string) error {
	player, err := s.playerRepo.FindByID(ctx, playerID)
	if err != nil {
		return fmt.Errorf("find player: %w", err)
	}

	ownedFactions, err := s.factionRepo.GetPlayerFactions(ctx, playerID)
	if err != nil {
		return fmt.Errorf("get player factions: %w", err)
	}
	factionSet := make(map[string]bool, len(ownedFactions))
	for _, f := range ownedFactions {
		factionSet[f] = true
	}

	completedIDs, err := s.storyRepo.GetCompletedEpisodeIDs(ctx, playerID)
	if err != nil {
		return fmt.Errorf("get completed episodes: %w", err)
	}
	completedSet := make(map[string]bool, len(completedIDs))
	for _, id := range completedIDs {
		completedSet[id] = true
	}

	if reason := checkUnlock(ep, player.Level, factionSet, completedSet); reason != nil {
		return ErrEpisodeLocked
	}
	return nil
}

func checkUnlock(ep *model.ScenarioEpisode, playerLevel int64, factionSet, completedSet map[string]bool) *model.LockReason {
	if playerLevel < ep.RequiredLevel {
		return &model.LockReason{
			Type:     "level",
			Required: ep.RequiredLevel,
			Current:  playerLevel,
		}
	}

	for _, f := range ep.RequiredFactions {
		if !factionSet[f] {
			return &model.LockReason{
				Type:     "faction",
				Required: f,
			}
		}
	}

	for _, reqEp := range ep.RequiredEpisodes {
		if !completedSet[reqEp] {
			return &model.LockReason{
				Type:     "episode",
				Required: reqEp,
			}
		}
	}

	return nil
}

func (s *StoryService) readScript(ctx context.Context, pathTemplate, lang string) (string, error) {
	path := strings.Replace(pathTemplate, "{lang}", lang, 1)

	// Local filesystem fallback when no GCS bucket is configured.
	if s.bucketName == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			// Fallback to Japanese if requested language is not found.
			if lang != "ja" {
				jaPath := strings.Replace(pathTemplate, "{lang}", "ja", 1)
				data, err = os.ReadFile(jaPath)
				if err != nil {
					return "", fmt.Errorf("read local script: %w", err)
				}
				return string(data), nil
			}
			return "", fmt.Errorf("read local script: %w", err)
		}
		return string(data), nil
	}

	rc, err := s.gcsClient.Bucket(s.bucketName).Object(path).NewReader(ctx)
	if err != nil {
		// Fallback to Japanese.
		if lang != "ja" {
			jaPath := strings.Replace(pathTemplate, "{lang}", "ja", 1)
			rc, err = s.gcsClient.Bucket(s.bucketName).Object(jaPath).NewReader(ctx)
			if err != nil {
				return "", fmt.Errorf("read gcs script: %w", err)
			}
		} else {
			return "", fmt.Errorf("read gcs script: %w", err)
		}
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read gcs script body: %w", err)
	}
	return string(data), nil
}

func episodeTitle(ep *model.ScenarioEpisode, lang string) string {
	if lang == "en" {
		return ep.TitleEn
	}
	return ep.TitleJa
}
