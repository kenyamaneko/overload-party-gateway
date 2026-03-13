package service

import (
	"context"
	"testing"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type testStoryEnv struct {
	svc         *StoryService
	storyRepo   *repository.MockStoryRepository
	factionRepo *repository.MockFactionRepository
	playerRepo  *repository.MockPlayerRepository
}

func newTestStoryEnv() *testStoryEnv {
	storyRepo := repository.NewMockStoryRepository()
	factionRepo := repository.NewMockFactionRepository()
	playerRepo := repository.NewMockPlayerRepository()

	svc := NewStoryService(storyRepo, factionRepo, playerRepo, nil, "")

	return &testStoryEnv{
		svc:         svc,
		storyRepo:   storyRepo,
		factionRepo: factionRepo,
		playerRepo:  playerRepo,
	}
}

func createStoryTestPlayer(env *testStoryEnv, playerID string, level int64) {
	_ = env.playerRepo.Create(context.Background(), &model.Player{
		PlayerID:    playerID,
		FirebaseUID: "uid-" + playerID,
		Level:       level,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, &model.PlayerDailyBattle{PlayerID: playerID})
}

func seedTestEpisodes(env *testStoryEnv) {
	faction := "SHE"
	env.storyRepo.SeedEpisodes([]*model.ScenarioEpisode{
		{
			EpisodeID:        "she_ep1",
			Faction:          &faction,
			EpisodeNumber:    1,
			TitleJa:          "SHE 第1章",
			TitleEn:          "SHE Chapter 1",
			RequiredLevel:    2,
			RequiredFactions: []string{"SHE"},
			RequiredEpisodes: []string{},
			ScriptPath:       "stories/{lang}/she_ep1.ks",
			SortOrder:        1,
			IsActive:         true,
		},
		{
			EpisodeID:        "she_ep2",
			Faction:          &faction,
			EpisodeNumber:    2,
			TitleJa:          "SHE 第2章",
			TitleEn:          "SHE Chapter 2",
			RequiredLevel:    6,
			RequiredFactions: []string{"SHE"},
			RequiredEpisodes: []string{"she_ep1"},
			ScriptPath:       "stories/{lang}/she_ep2.ks",
			SortOrder:        5,
			IsActive:         true,
		},
		{
			EpisodeID:        "inactive_ep",
			Faction:          &faction,
			EpisodeNumber:    99,
			TitleJa:          "非公開エピソード",
			TitleEn:          "Inactive Episode",
			RequiredLevel:    1,
			RequiredFactions: []string{},
			RequiredEpisodes: []string{},
			ScriptPath:       "stories/{lang}/inactive.ks",
			SortOrder:        99,
			IsActive:         false,
		},
	})
}

// ---------------------------------------------------------------------------
// ListEpisodes tests
// ---------------------------------------------------------------------------

func TestListEpisodes_UnlockedAndLocked(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 5)
	seedTestEpisodes(env)

	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	// inactive episodes are excluded by mock
	require.Len(t, episodes, 2)

	t.Run("first episode is unlocked", func(t *testing.T) {
		assert.Equal(t, "she_ep1", episodes[0].EpisodeID)
		assert.True(t, episodes[0].IsUnlocked)
		assert.Nil(t, episodes[0].LockReason)
		assert.Equal(t, "SHE 第1章", episodes[0].Title)
	})

	t.Run("second episode locked by level", func(t *testing.T) {
		assert.Equal(t, "she_ep2", episodes[1].EpisodeID)
		assert.False(t, episodes[1].IsUnlocked)
		require.NotNil(t, episodes[1].LockReason)
		assert.Equal(t, "level", episodes[1].LockReason.Type)
	})
}

func TestListEpisodes_LockedByFaction(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedTestEpisodes(env)

	// No factions owned

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	require.Len(t, episodes, 2)
	assert.False(t, episodes[0].IsUnlocked)
	require.NotNil(t, episodes[0].LockReason)
	assert.Equal(t, "faction", episodes[0].LockReason.Type)
	assert.Equal(t, "SHE", episodes[0].LockReason.Required)
}

func TestListEpisodes_LockedByEpisode(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedTestEpisodes(env)

	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")
	// she_ep1 NOT completed → she_ep2 locked by episode

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	ep2 := episodes[1]
	assert.False(t, ep2.IsUnlocked)
	require.NotNil(t, ep2.LockReason)
	assert.Equal(t, "episode", ep2.LockReason.Type)
}

func TestListEpisodes_EnglishTitle(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedTestEpisodes(env)
	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "en")
	require.NoError(t, err)

	assert.Equal(t, "SHE Chapter 1", episodes[0].Title)
}

func TestListEpisodes_CompletedStatus(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedTestEpisodes(env)
	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")
	_ = env.storyRepo.MarkComplete(context.Background(), "p1", "she_ep1")

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	assert.True(t, episodes[0].IsCompleted)
	assert.False(t, episodes[1].IsCompleted)

	t.Run("completing ep1 unlocks ep2", func(t *testing.T) {
		assert.True(t, episodes[1].IsUnlocked)
	})
}

// ---------------------------------------------------------------------------
// CompleteEpisode tests
// ---------------------------------------------------------------------------

func TestCompleteEpisode_Success(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 5)
	seedTestEpisodes(env)
	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")

	err := env.svc.CompleteEpisode(context.Background(), "p1", "she_ep1")
	require.NoError(t, err)

	ids, _ := env.storyRepo.GetCompletedEpisodeIDs(context.Background(), "p1")
	assert.Contains(t, ids, "she_ep1")
}

func TestCompleteEpisode_Idempotent(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 5)
	seedTestEpisodes(env)
	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")

	err := env.svc.CompleteEpisode(context.Background(), "p1", "she_ep1")
	require.NoError(t, err)

	err = env.svc.CompleteEpisode(context.Background(), "p1", "she_ep1")
	require.NoError(t, err)

	ids, _ := env.storyRepo.GetCompletedEpisodeIDs(context.Background(), "p1")
	count := 0
	for _, id := range ids {
		if id == "she_ep1" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCompleteEpisode_NotFound(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 5)
	seedTestEpisodes(env)

	err := env.svc.CompleteEpisode(context.Background(), "p1", "nonexistent")
	assert.ErrorIs(t, err, ErrEpisodeNotFound)
}

func TestCompleteEpisode_Locked(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 1) // level too low
	seedTestEpisodes(env)

	err := env.svc.CompleteEpisode(context.Background(), "p1", "she_ep1")
	assert.ErrorIs(t, err, ErrEpisodeLocked)
}

func TestCompleteEpisode_InactiveEpisode(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 99)
	seedTestEpisodes(env)

	err := env.svc.CompleteEpisode(context.Background(), "p1", "inactive_ep")
	assert.ErrorIs(t, err, ErrEpisodeNotFound)
}

// ---------------------------------------------------------------------------
// checkUnlock tests
// ---------------------------------------------------------------------------

func TestCheckUnlock_AllConditionsMet(t *testing.T) {
	faction := "SHE"
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}
	ep.Faction = &faction

	factionSet := map[string]bool{"SHE": true}
	completedSet := map[string]bool{"she_ep1": true}

	reason := checkUnlock(ep, 10, factionSet, completedSet)
	assert.Nil(t, reason)
}

func TestCheckUnlock_LevelPriority(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	// All conditions unmet — level is checked first
	reason := checkUnlock(ep, 1, map[string]bool{}, map[string]bool{})
	require.NotNil(t, reason)
	assert.Equal(t, "level", reason.Type)
}

func TestCheckUnlock_FactionAfterLevel(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	// Level met, faction not
	reason := checkUnlock(ep, 10, map[string]bool{}, map[string]bool{})
	require.NotNil(t, reason)
	assert.Equal(t, "faction", reason.Type)
}

func TestCheckUnlock_EpisodeAfterFaction(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	// Level + faction met, episode not
	reason := checkUnlock(ep, 10, map[string]bool{"SHE": true}, map[string]bool{})
	require.NotNil(t, reason)
	assert.Equal(t, "episode", reason.Type)
}
