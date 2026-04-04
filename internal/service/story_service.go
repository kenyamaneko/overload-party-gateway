package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var (
	ErrEpisodeNotFound = fmt.Errorf("episode not found")
	ErrEpisodeLocked   = fmt.Errorf("episode is locked")
)

type StoryService struct {
	storyRepo  port.StoryRepo
	gcsClient  *storage.Client
	bucketName string
}

// NewStoryService creates a new StoryService.
func NewStoryService(
	storyRepo port.StoryRepo,
	gcsClient *storage.Client,
	bucketName string,
) *StoryService {
	return &StoryService{
		storyRepo:  storyRepo,
		gcsClient:  gcsClient,
		bucketName: bucketName,
	}
}

func (s *StoryService) ListEpisodes(ctx context.Context, playerID, lang string) ([]model.EpisodeWithStatus, error) {
	episodes, err := s.storyRepo.ListActiveEpisodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}

	uc, err := s.storyRepo.GetUnlockContext(ctx, playerID)
	if err != nil {
		return nil, fmt.Errorf("get unlock context: %w", err)
	}

	result := make([]model.EpisodeWithStatus, 0, len(episodes))
	for _, ep := range episodes {
		reasons := checkUnlock(ep, uc)
		status := model.EpisodeWithStatus{
			EpisodeID:     ep.EpisodeID,
			Faction:       ep.Faction,
			EpisodeNumber: ep.EpisodeNumber,
			Title:         episodeTitle(ep, lang),
			IsCompleted:   uc.CompletedEpisodes[ep.EpisodeID],
			IsUnlocked:    len(reasons) == 0,
			LockReasons:   reasons,
		}

		result = append(result, status)
	}
	return result, nil
}

func (s *StoryService) GetScript(ctx context.Context, playerID, episodeID, lang string) (string, error) {
	ep, err := s.storyRepo.FindEpisodeByID(ctx, episodeID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return "", ErrEpisodeNotFound
		}
		return "", fmt.Errorf("find episode: %w", err)
	}
	if !ep.IsActive {
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
		if errors.Is(err, port.ErrNotFound) {
			return ErrEpisodeNotFound
		}
		return fmt.Errorf("find episode: %w", err)
	}
	if !ep.IsActive {
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
	uc, err := s.storyRepo.GetUnlockContext(ctx, playerID)
	if err != nil {
		return fmt.Errorf("get unlock context: %w", err)
	}

	if reasons := checkUnlock(ep, uc); len(reasons) > 0 {
		return ErrEpisodeLocked
	}
	return nil
}

func checkUnlock(ep *model.ScenarioEpisode, uc *model.StoryUnlockContext) []model.LockReason {
	var reasons []model.LockReason

	if uc.PlayerLevel < ep.RequiredLevel {
		reasons = append(reasons, model.NewLockReasonLevel(ep.RequiredLevel, uc.PlayerLevel))
	}

	for _, f := range ep.RequiredFactions {
		if !uc.OwnedFactions[f] {
			reasons = append(reasons, model.NewLockReasonFaction(f))
		}
	}

	for _, reqEp := range ep.RequiredEpisodes {
		if !uc.CompletedEpisodes[reqEp] {
			reasons = append(reasons, model.NewLockReasonEpisode(reqEp))
		}
	}

	return reasons
}

func (s *StoryService) readScript(ctx context.Context, pathTemplate, lang string) (string, error) {
	path := strings.Replace(pathTemplate, "{lang}", lang, 1)

	// Local filesystem fallback when no GCS bucket is configured.
	if s.bucketName == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			// Fallback to Japanese only when the requested file does not exist.
			if lang != "ja" && os.IsNotExist(err) {
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
		// Fallback to Japanese only for ErrObjectNotExist; other errors (permission, network) propagate.
		if lang != "ja" && errors.Is(err, storage.ErrObjectNotExist) {
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
