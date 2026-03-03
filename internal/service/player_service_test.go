package service

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/civil"

	"github.com/kenyamaneko/overload-party-common/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// mockGameConfigRepo is a simple mock that returns configured values.
type mockGameConfigRepo struct {
	values map[string]int64
}

func (m *mockGameConfigRepo) GetInt64(ctx context.Context, key string, fallback int64) (int64, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return fallback, nil
}

// today returns the current game day (UTC+4h), matching the gameDay() logic in player_service.go.
func today() civil.Date {
	return civil.DateOf(time.Now().UTC().Add(4 * time.Hour))
}

// yesterday returns one day before the current game day.
func yesterday() civil.Date {
	d := today()
	return civil.Date{Year: d.Year, Month: d.Month, Day: d.Day - 1}
}

// setupPlayerService creates a PlayerService with a MockPlayerRepository and mockGameConfigRepo,
// and registers a player with the given daily battle data.
func setupPlayerService(t *testing.T, player *model.Player, dailyBattle *model.PlayerDailyBattle) *PlayerService {
	t.Helper()

	playerRepo := repository.NewMockPlayerRepository()
	if err := playerRepo.Create(context.Background(), player, dailyBattle); err != nil {
		t.Fatalf("failed to create player: %v", err)
	}

	configRepo := &mockGameConfigRepo{
		values: map[string]int64{
			configKeyFreeDailyBattleLimit:    10,
			configKeyPremiumDailyBattleLimit: 30,
		},
	}

	return NewPlayerService(playerRepo, configRepo)
}

// --- GetBattleLimit tests ---

func TestGetBattleLimit_FreePlayer_UnderLimit(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 3, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.DailyBattleCount != 3 {
		t.Errorf("DailyBattleCount = %d, want 3", resp.DailyBattleCount)
	}
	if resp.DailyBattleLimit != 10 {
		t.Errorf("DailyBattleLimit = %d, want 10", resp.DailyBattleLimit)
	}
	if !resp.CanBattle {
		t.Errorf("CanBattle = false, want true")
	}
}

func TestGetBattleLimit_FreePlayer_AtLimit(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 10, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.DailyBattleCount != 10 {
		t.Errorf("DailyBattleCount = %d, want 10", resp.DailyBattleCount)
	}
	if resp.DailyBattleLimit != 10 {
		t.Errorf("DailyBattleLimit = %d, want 10", resp.DailyBattleLimit)
	}
	if resp.CanBattle {
		t.Errorf("CanBattle = true, want false")
	}
}

func TestGetBattleLimit_PremiumPlayer(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: true}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 5, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.DailyBattleCount != 5 {
		t.Errorf("DailyBattleCount = %d, want 5", resp.DailyBattleCount)
	}
	if resp.DailyBattleLimit != 30 {
		t.Errorf("DailyBattleLimit = %d, want 30", resp.DailyBattleLimit)
	}
	if !resp.CanBattle {
		t.Errorf("CanBattle = false, want true")
	}
}

func TestGetBattleLimit_DateReset(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 7, LastResetDate: yesterday()}

	svc := setupPlayerService(t, player, daily)

	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.DailyBattleCount != 0 {
		t.Errorf("DailyBattleCount = %d, want 0 (date reset)", resp.DailyBattleCount)
	}
	if resp.DailyBattleLimit != 10 {
		t.Errorf("DailyBattleLimit = %d, want 10", resp.DailyBattleLimit)
	}
	if !resp.CanBattle {
		t.Errorf("CanBattle = false, want true (count reset to 0)")
	}
}

// --- CheckAndIncrementBattleCount tests ---

func TestCheckAndIncrementBattleCount_Success(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 5, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	err := svc.CheckAndIncrementBattleCount(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify count was incremented by checking via GetBattleLimit
	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error on GetBattleLimit: %v", err)
	}
	if resp.DailyBattleCount != 6 {
		t.Errorf("DailyBattleCount = %d, want 6 (incremented from 5)", resp.DailyBattleCount)
	}
}

func TestCheckAndIncrementBattleCount_LimitReached(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 10, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	err := svc.CheckAndIncrementBattleCount(context.Background(), "p1")
	if err == nil {
		t.Fatal("expected error for limit reached, got nil")
	}

	expected := "daily battle limit reached"
	if got := err.Error(); !contains(got, expected) {
		t.Errorf("error = %q, want it to contain %q", got, expected)
	}
}

func TestCheckAndIncrementBattleCount_PremiumPlayer(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: true}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 29, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	err := svc.CheckAndIncrementBattleCount(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify count was incremented
	resp, err := svc.GetBattleLimit(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error on GetBattleLimit: %v", err)
	}
	if resp.DailyBattleCount != 30 {
		t.Errorf("DailyBattleCount = %d, want 30 (incremented from 29)", resp.DailyBattleCount)
	}
}

func TestCheckAndIncrementBattleCount_PremiumLimitReached(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", IsPremium: true}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 30, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	err := svc.CheckAndIncrementBattleCount(context.Background(), "p1")
	if err == nil {
		t.Fatal("expected error for premium limit reached, got nil")
	}

	expected := "daily battle limit reached"
	if got := err.Error(); !contains(got, expected) {
		t.Errorf("error = %q, want it to contain %q", got, expected)
	}
}

// --- GetPlayer / UpdateUsername tests ---

func TestGetPlayer_Success(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Username: "Alice"}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	got, err := svc.GetPlayer(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.PlayerID != "p1" {
		t.Errorf("PlayerID = %q, want %q", got.PlayerID, "p1")
	}
	if got.Username != "Alice" {
		t.Errorf("Username = %q, want %q", got.Username, "Alice")
	}
}

func TestUpdateUsername_Success(t *testing.T) {
	player := &model.Player{PlayerID: "p1", FirebaseUID: "uid1", Username: "Alice"}
	daily := &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()}

	svc := setupPlayerService(t, player, daily)

	updated, err := svc.UpdateUsername(context.Background(), "p1", "Bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated.Username != "Bob" {
		t.Errorf("Username = %q, want %q", updated.Username, "Bob")
	}

	// Verify the name persists
	got, err := svc.GetPlayer(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error on GetPlayer: %v", err)
	}
	if got.Username != "Bob" {
		t.Errorf("Username after re-fetch = %q, want %q", got.Username, "Bob")
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
