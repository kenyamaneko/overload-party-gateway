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

	svc := NewStoryService(storyRepo, nil, "")

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

// findReasonByType returns the first LockReason with the given type, or nil.
func findReasonByType(reasons []model.LockReason, typ string) *model.LockReason {
	for i := range reasons {
		if reasons[i].Type == typ {
			return &reasons[i]
		}
	}
	return nil
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

	// First episode: unlocked
	assert.Equal(t, "she_ep1", episodes[0].EpisodeID)
	assert.True(t, episodes[0].IsUnlocked)
	assert.Empty(t, episodes[0].LockReasons)
	assert.Equal(t, "SHE 第1章", episodes[0].Title)

	// Second episode: locked by level
	assert.Equal(t, "she_ep2", episodes[1].EpisodeID)
	assert.False(t, episodes[1].IsUnlocked)
	require.NotEmpty(t, episodes[1].LockReasons)
	assert.NotNil(t, findReasonByType(episodes[1].LockReasons, "level"))
}

func TestListEpisodes_LockedByFaction(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedTestEpisodes(env)

	episodes, err := env.svc.ListEpisodes(context.Background(), "p1", "ja")
	require.NoError(t, err)

	require.Len(t, episodes, 2)
	assert.False(t, episodes[0].IsUnlocked)
	require.NotEmpty(t, episodes[0].LockReasons)
	r := findReasonByType(episodes[0].LockReasons, "faction")
	require.NotNil(t, r)
	assert.Equal(t, "SHE", r.Required)
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
	require.NotEmpty(t, ep2.LockReasons)
	assert.NotNil(t, findReasonByType(ep2.LockReasons, "episode"))
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
	require.NotEmpty(t, final.LockReasons)
	assert.NotNil(t, findReasonByType(final.LockReasons, "episode"))
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
	require.NotEmpty(t, final.LockReasons)
	assert.NotNil(t, findReasonByType(final.LockReasons, "faction"))
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
	require.NotEmpty(t, final.LockReasons)
	r := findReasonByType(final.LockReasons, "level")
	require.NotNil(t, r)
	assert.Equal(t, int64(22), r.Required)
	assert.Equal(t, int64(10), r.Current)
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
	assert.Empty(t, final.LockReasons)
	assert.Nil(t, final.Faction)
	assert.Equal(t, "最終話", final.Title)
}

// ---------------------------------------------------------------------------
// Grand Ending: multiple lock reasons returned at once
// ---------------------------------------------------------------------------

func TestListEpisodes_GrandEnding_MultipleLockReasons(t *testing.T) {
	env := newTestStoryEnv()
	createStoryTestPlayer(env, "p1", 10)
	seedAllFactionEpisodes(env)

	// Own only SHE, complete no episodes → level + 3 factions + 4 episodes = 8 reasons
	_ = env.factionRepo.AddPlayerFaction(context.Background(), "p1", "SHE", "initial_selection")

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

	assert.NotNil(t, findReasonByType(final.LockReasons, "level"))
	assert.NotNil(t, findReasonByType(final.LockReasons, "faction"))
	assert.NotNil(t, findReasonByType(final.LockReasons, "episode"))

	// Count: 1 level + 3 factions (Tenki, Sugar, Tuners) + 4 episodes
	assert.Len(t, final.LockReasons, 8)
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

	reasons := checkUnlock(ep, uc)
	assert.Empty(t, reasons)
}

func TestCheckUnlock_AllConditionsUnmet(t *testing.T) {
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

	reasons := checkUnlock(ep, uc)
	require.Len(t, reasons, 3)
	assert.Equal(t, "level", reasons[0].Type)
	assert.Equal(t, int64(5), reasons[0].Required)
	assert.Equal(t, int64(1), reasons[0].Current)
	assert.Equal(t, "faction", reasons[1].Type)
	assert.Equal(t, "SHE", reasons[1].Required)
	assert.Equal(t, "episode", reasons[2].Type)
	assert.Equal(t, "she_ep1", reasons[2].Required)
}

func TestCheckUnlock_LevelOnly(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	uc := &model.StoryUnlockContext{
		PlayerLevel:       1,
		OwnedFactions:     map[string]bool{"SHE": true},
		CompletedEpisodes: map[string]bool{"she_ep1": true},
	}

	reasons := checkUnlock(ep, uc)
	require.Len(t, reasons, 1)
	assert.Equal(t, "level", reasons[0].Type)
}

func TestCheckUnlock_FactionOnly(t *testing.T) {
	ep := &model.ScenarioEpisode{
		RequiredLevel:    5,
		RequiredFactions: []string{"SHE"},
		RequiredEpisodes: []string{"she_ep1"},
	}

	uc := &model.StoryUnlockContext{
		PlayerLevel:       10,
		OwnedFactions:     map[string]bool{},
		CompletedEpisodes: map[string]bool{"she_ep1": true},
	}

	reasons := checkUnlock(ep, uc)
	require.Len(t, reasons, 1)
	assert.Equal(t, "faction", reasons[0].Type)
}

func TestCheckUnlock_EpisodeOnly(t *testing.T) {
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

	reasons := checkUnlock(ep, uc)
	require.Len(t, reasons, 1)
	assert.Equal(t, "episode", reasons[0].Type)
	assert.Equal(t, "she_ep1", reasons[0].Required)
}
