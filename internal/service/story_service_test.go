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
	storyRepo.SetDeps(playerRepo, factionRepo)

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

func seedAllFactionEpisodes(env *testStoryEnv) {
	she := "SHE"
	tenki := "Tenki"
	sugar := "Sugar"
	tuners := "Tuners"

	episodes := []*model.ScenarioEpisode{
		{EpisodeID: "she_ep1", Faction: &she, EpisodeNumber: 1, TitleJa: "SHE 1", TitleEn: "SHE 1", RequiredLevel: 2, RequiredFactions: []string{"SHE"}, RequiredEpisodes: []string{}, ScriptPath: "stories/{lang}/she_ep1.ks", SortOrder: 1, IsActive: true},
		{EpisodeID: "she_ep5", Faction: &she, EpisodeNumber: 5, TitleJa: "SHE 5", TitleEn: "SHE 5", RequiredLevel: 18, RequiredFactions: []string{"SHE"}, RequiredEpisodes: []string{"she_ep1"}, ScriptPath: "stories/{lang}/she_ep5.ks", SortOrder: 17, IsActive: true},
		{EpisodeID: "tenki_ep5", Faction: &tenki, EpisodeNumber: 5, TitleJa: "天気 5", TitleEn: "Tenki 5", RequiredLevel: 19, RequiredFactions: []string{"Tenki"}, RequiredEpisodes: []string{}, ScriptPath: "stories/{lang}/tenki_ep5.ks", SortOrder: 18, IsActive: true},
		{EpisodeID: "sugar_ep5", Faction: &sugar, EpisodeNumber: 5, TitleJa: "Sugar 5", TitleEn: "Sugar 5", RequiredLevel: 20, RequiredFactions: []string{"Sugar"}, RequiredEpisodes: []string{}, ScriptPath: "stories/{lang}/sugar_ep5.ks", SortOrder: 19, IsActive: true},
		{EpisodeID: "tuners_ep5", Faction: &tuners, EpisodeNumber: 5, TitleJa: "Tuners 5", TitleEn: "Tuners 5", RequiredLevel: 21, RequiredFactions: []string{"Tuners"}, RequiredEpisodes: []string{}, ScriptPath: "stories/{lang}/tuners_ep5.ks", SortOrder: 20, IsActive: true},
		{EpisodeID: "final", Faction: nil, EpisodeNumber: 1, TitleJa: "最終話", TitleEn: "Final Chapter", RequiredLevel: 22, RequiredFactions: []string{"SHE", "Tenki", "Sugar", "Tuners"}, RequiredEpisodes: []string{"she_ep5", "tenki_ep5", "sugar_ep5", "tuners_ep5"}, ScriptPath: "stories/{lang}/final.ks", SortOrder: 21, IsActive: true},
	}

	env.storyRepo.SeedEpisodes(episodes)
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
// Grand Ending unlock tests
// ---------------------------------------------------------------------------

func TestListEpisodes_GrandEnding_Locked(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 25)
	seedAllFactionEpisodes(env)

	for _, f := range []string{"SHE", "Tenki", "Sugar", "Tuners"} {
		_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", f, "initial_selection")
	}

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	var final *model.EpisodeWithStatus
	for i := range episodes {
		if episodes[i].EpisodeID == "final" {
			final = &episodes[i]
			break
		}
	}
	require.NotNil(t, final)
	assert.False(t, final.IsUnlocked)
	require.NotNil(t, final.LockReason)
	assert.Equal(t, "episode", final.LockReason.Type)
}

func TestListEpisodes_GrandEnding_LockedByFaction(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 25)
	seedAllFactionEpisodes(env)

	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")

	for _, ep := range []string{"she_ep1", "she_ep5", "tenki_ep5", "sugar_ep5", "tuners_ep5"} {
		_ = env.storyRepo.MarkComplete(context.Background(), "p1", ep)
	}

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	var final *model.EpisodeWithStatus
	for i := range episodes {
		if episodes[i].EpisodeID == "final" {
			final = &episodes[i]
			break
		}
	}
	require.NotNil(t, final)
	assert.False(t, final.IsUnlocked)
	require.NotNil(t, final.LockReason)
	assert.Equal(t, "faction", final.LockReason.Type)
}

func TestListEpisodes_GrandEnding_LockedByLevel(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedAllFactionEpisodes(env)

	for _, f := range []string{"SHE", "Tenki", "Sugar", "Tuners"} {
		_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", f, "initial_selection")
	}
	for _, ep := range []string{"she_ep1", "she_ep5", "tenki_ep5", "sugar_ep5", "tuners_ep5"} {
		_ = env.storyRepo.MarkComplete(context.Background(), "p1", ep)
	}

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	var final *model.EpisodeWithStatus
	for i := range episodes {
		if episodes[i].EpisodeID == "final" {
			final = &episodes[i]
			break
		}
	}
	require.NotNil(t, final)
	assert.False(t, final.IsUnlocked)
	require.NotNil(t, final.LockReason)
	assert.Equal(t, "level", final.LockReason.Type)
	assert.Equal(t, "22", final.LockReason.Required)
	assert.Equal(t, "10", final.LockReason.Current)
}

func TestListEpisodes_GrandEnding_Unlocked(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 25)
	seedAllFactionEpisodes(env)

	for _, f := range []string{"SHE", "Tenki", "Sugar", "Tuners"} {
		_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", f, "initial_selection")
	}
	for _, ep := range []string{"she_ep1", "she_ep5", "tenki_ep5", "sugar_ep5", "tuners_ep5"} {
		_ = env.storyRepo.MarkComplete(context.Background(), "p1", ep)
	}

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	var final *model.EpisodeWithStatus
	for i := range episodes {
		if episodes[i].EpisodeID == "final" {
			final = &episodes[i]
			break
		}
	}
	require.NotNil(t, final)
	assert.True(t, final.IsUnlocked)
	assert.Nil(t, final.LockReason)
	assert.Nil(t, final.Faction)
	assert.Equal(t, "最終話", final.Title)
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
	createStoryTestPlayer(env, "p1", 1)
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
// GetScript tests (validates unlock + not-found paths; no file IO)
// ---------------------------------------------------------------------------

func TestGetScript_NotFound(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedTestEpisodes(env)

	_, err := env.svc.GetScript(context.Background(), "p1", "nonexistent", "ja")
	assert.ErrorIs(t, err, ErrEpisodeNotFound)
}

func TestGetScript_InactiveEpisode(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 99)
	seedTestEpisodes(env)

	_, err := env.svc.GetScript(context.Background(), "p1", "inactive_ep", "ja")
	assert.ErrorIs(t, err, ErrEpisodeNotFound)
}

func TestGetScript_Locked(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 1)
	seedTestEpisodes(env)

	_, err := env.svc.GetScript(context.Background(), "p1", "she_ep1", "ja")
	assert.ErrorIs(t, err, ErrEpisodeLocked)
}

// ---------------------------------------------------------------------------
// checkUnlock tests
// ---------------------------------------------------------------------------

func TestCheckUnlock_AllConditionsMet(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	uc := &model.StoryUnlockContext{
		PlayerLevel:       10,
		OwnedFactions:     map[string]bool{"SHE": true},
		CompletedEpisodes: map[string]bool{"she_ep1": true},
	}

	reason := checkUnlock(ep, uc)
	assert.Nil(t, reason)
}

func TestCheckUnlock_LevelPriority(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	uc := &model.StoryUnlockContext{
		PlayerLevel:       1,
		OwnedFactions:     map[string]bool{},
		CompletedEpisodes: map[string]bool{},
	}

	reason := checkUnlock(ep, uc)
	require.NotNil(t, reason)
	assert.Equal(t, "level", reason.Type)
	assert.Equal(t, "5", reason.Required)
	assert.Equal(t, "1", reason.Current)
}

func TestCheckUnlock_FactionAfterLevel(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	uc := &model.StoryUnlockContext{
		PlayerLevel:       10,
		OwnedFactions:     map[string]bool{},
		CompletedEpisodes: map[string]bool{},
	}

	reason := checkUnlock(ep, uc)
	require.NotNil(t, reason)
	assert.Equal(t, "faction", reason.Type)
}

func TestCheckUnlock_EpisodeAfterFaction(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	uc := &model.StoryUnlockContext{
		PlayerLevel:       10,
		OwnedFactions:     map[string]bool{"SHE": true},
		CompletedEpisodes: map[string]bool{},
	}

	reason := checkUnlock(ep, uc)
	require.NotNil(t, reason)
	assert.Equal(t, "episode", reason.Type)
	assert.Equal(t, "she_ep1", reason.Required)
}
