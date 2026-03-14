package model

import (
	"fmt"
	"time"
)

// ScenarioEpisode represents a story episode definition.
type ScenarioEpisode struct {
	EpisodeID        string   `json:"episode_id" db:"episode_id"`
	Faction          *string  `json:"faction" db:"faction"`
	EpisodeNumber    int64    `json:"episode_number" db:"episode_number"`
	TitleJa          string   `json:"title_ja" db:"title_ja"`
	TitleEn          string   `json:"title_en" db:"title_en"`
	RequiredLevel    int64    `json:"required_level" db:"required_level"`
	RequiredFactions []string `json:"required_factions" db:"required_factions"`
	RequiredEpisodes []string `json:"required_episodes" db:"required_episodes"`
	ScriptPath       string   `json:"-" db:"script_path"`
	ThumbnailPath    *string  `json:"-" db:"thumbnail_path"`
	SortOrder        int64    `json:"sort_order" db:"sort_order"`
	IsActive         bool     `json:"-" db:"is_active"`
	CreatedAt        time.Time `json:"-" db:"created_at"`
}

// PlayerStoryProgress records that a player has completed an episode.
type PlayerStoryProgress struct {
	PlayerID    string    `json:"player_id" db:"player_id"`
	EpisodeID   string    `json:"episode_id" db:"episode_id"`
	CompletedAt time.Time `json:"completed_at" db:"completed_at"`
}

// PlayerFaction records that a player owns a faction's card set.
type PlayerFaction struct {
	PlayerID   string    `json:"player_id" db:"player_id"`
	Faction    string    `json:"faction" db:"faction"`
	Source     string    `json:"source" db:"source"`
	AcquiredAt time.Time `json:"acquired_at" db:"acquired_at"`
}

// EpisodeWithStatus is the API response for a single episode with unlock status.
type EpisodeWithStatus struct {
	EpisodeID     string      `json:"episode_id"`
	Faction       *string     `json:"faction"`
	EpisodeNumber int64       `json:"episode_number"`
	Title         string      `json:"title"`
	ThumbnailURL  *string     `json:"thumbnail_url"`
	IsUnlocked    bool        `json:"is_unlocked"`
	IsCompleted   bool        `json:"is_completed"`
	LockReason    *LockReason `json:"lock_reason"`
}

// LockReason describes why an episode is locked.
// Required and Current are always strings for type safety and consistent JSON serialisation.
type LockReason struct {
	Type     string `json:"type"`               // "level" | "faction" | "episode"
	Required string `json:"required"`
	Current  string `json:"current,omitempty"`
}

// StoryUnlockContext holds pre-fetched data needed for episode unlock checks.
type StoryUnlockContext struct {
	PlayerLevel      int64
	OwnedFactions    map[string]bool
	CompletedEpisodes map[string]bool
}

// NewLockReasonLevel creates a level lock reason.
func NewLockReasonLevel(required, current int64) *LockReason {
	return &LockReason{Type: "level", Required: fmt.Sprintf("%d", required), Current: fmt.Sprintf("%d", current)}
}

// NewLockReasonFaction creates a faction lock reason.
func NewLockReasonFaction(faction string) *LockReason {
	return &LockReason{Type: "faction", Required: faction}
}

// NewLockReasonEpisode creates an episode prerequisite lock reason.
func NewLockReasonEpisode(episodeID string) *LockReason {
	return &LockReason{Type: "episode", Required: episodeID}
}
