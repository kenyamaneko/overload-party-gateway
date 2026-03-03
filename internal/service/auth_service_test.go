package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// stampTrackingShopRepo wraps MockShopRepository to record inserted player items.
type stampTrackingShopRepo struct {
	*repository.MockShopRepository
	insertedItems []*model.PlayerItem
}

func newStampTrackingShopRepo() *stampTrackingShopRepo {
	return &stampTrackingShopRepo{
		MockShopRepository: repository.NewMockShopRepository(),
	}
}

func (r *stampTrackingShopRepo) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	r.insertedItems = append(r.insertedItems, items...)
	return nil
}

// failingStampShopRepo wraps MockShopRepository but returns an error from InsertPlayerItems.
type failingStampShopRepo struct {
	*repository.MockShopRepository
}

func (r *failingStampShopRepo) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	return fmt.Errorf("database connection lost")
}

// --- Register tests ---

func TestRegister_Success(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	player, err := svc.Register(context.Background(), "firebase-uid-1", "TestUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify player fields
	if player.PlayerID == "" {
		t.Error("expected non-empty PlayerID")
	}
	if player.FirebaseUID != "firebase-uid-1" {
		t.Errorf("expected FirebaseUID=firebase-uid-1, got %s", player.FirebaseUID)
	}
	if player.Username != "TestUser" {
		t.Errorf("expected Username=TestUser, got %s", player.Username)
	}
	if player.Level != 1 {
		t.Errorf("expected Level=1, got %d", player.Level)
	}
	if player.Exp != 0 {
		t.Errorf("expected Exp=0, got %d", player.Exp)
	}
	if player.Wins != 0 {
		t.Errorf("expected Wins=0, got %d", player.Wins)
	}
	if player.Losses != 0 {
		t.Errorf("expected Losses=0, got %d", player.Losses)
	}
	if player.IsPremium {
		t.Error("expected IsPremium=false")
	}

	// Verify player was persisted in the repo
	found, err := playerRepo.FindByFirebaseUID(context.Background(), "firebase-uid-1")
	if err != nil {
		t.Fatalf("FindByFirebaseUID failed: %v", err)
	}
	if found == nil {
		t.Fatal("player not found in repo after Register")
	}
	if found.PlayerID != player.PlayerID {
		t.Errorf("repo player ID mismatch: got %s, want %s", found.PlayerID, player.PlayerID)
	}

	// Verify PlayerDailyBattle record created with Count=0
	daily, err := playerRepo.GetDailyBattle(context.Background(), player.PlayerID)
	if err != nil {
		t.Fatalf("GetDailyBattle failed: %v", err)
	}
	if daily.DailyBattleCount != 0 {
		t.Errorf("expected DailyBattleCount=0, got %d", daily.DailyBattleCount)
	}

	// Verify default UserSettings created
	settings, err := userSettingsRepo.Get(context.Background(), player.PlayerID)
	if err != nil {
		t.Fatalf("Get UserSettings failed: %v", err)
	}
	if settings == nil {
		t.Fatal("expected default UserSettings to be created")
	}
	if settings.Language != "ja" {
		t.Errorf("expected Language=ja, got %s", settings.Language)
	}
	if settings.BgmVolume != 50 {
		t.Errorf("expected BgmVolume=50, got %d", settings.BgmVolume)
	}
	if settings.SeVolume != 50 {
		t.Errorf("expected SeVolume=50, got %d", settings.SeVolume)
	}
	if !settings.PushEnabled {
		t.Error("expected PushEnabled=true")
	}

	// Verify 7 starter stamps granted (stamp 1-7)
	if len(shopRepo.insertedItems) != 7 {
		t.Fatalf("expected 7 starter stamps, got %d", len(shopRepo.insertedItems))
	}
	for i, item := range shopRepo.insertedItems {
		expectedNo := int64(i + 1)
		if item.ItemType != "stamp" {
			t.Errorf("stamp %d: expected ItemType=stamp, got %s", expectedNo, item.ItemType)
		}
		if item.ItemNo != expectedNo {
			t.Errorf("stamp %d: expected ItemNo=%d, got %d", expectedNo, expectedNo, item.ItemNo)
		}
		if item.PlayerID != player.PlayerID {
			t.Errorf("stamp %d: expected PlayerID=%s, got %s", expectedNo, player.PlayerID, item.PlayerID)
		}
	}
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	// Register the first time — should succeed
	_, err := svc.Register(context.Background(), "firebase-uid-dup", "FirstUser")
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Register again with the same UID — should fail
	_, err = svc.Register(context.Background(), "firebase-uid-dup", "SecondUser")
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !strings.Contains(err.Error(), "player already registered") {
		t.Errorf("expected 'player already registered' error, got: %v", err)
	}
}

func TestRegister_StampFailure_NotFatal(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := &failingStampShopRepo{MockShopRepository: repository.NewMockShopRepository()}
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	// Register should succeed even though stamp insertion fails
	player, err := svc.Register(context.Background(), "firebase-uid-stamp-fail", "StampFailUser")
	if err != nil {
		t.Fatalf("expected Register to succeed despite stamp failure, got: %v", err)
	}

	// Verify player was still created
	if player.PlayerID == "" {
		t.Error("expected non-empty PlayerID")
	}
	if player.Username != "StampFailUser" {
		t.Errorf("expected Username=StampFailUser, got %s", player.Username)
	}

	// Verify player exists in the repo
	found, err := playerRepo.FindByFirebaseUID(context.Background(), "firebase-uid-stamp-fail")
	if err != nil {
		t.Fatalf("FindByFirebaseUID failed: %v", err)
	}
	if found == nil {
		t.Fatal("player should exist in repo despite stamp failure")
	}

	// Verify UserSettings were still created
	settings, err := userSettingsRepo.Get(context.Background(), player.PlayerID)
	if err != nil {
		t.Fatalf("Get UserSettings failed: %v", err)
	}
	if settings == nil {
		t.Fatal("expected UserSettings to be created despite stamp failure")
	}
}

// --- Login tests ---

func TestLogin_Success(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	// Register a player first
	registered, err := svc.Register(context.Background(), "firebase-uid-login", "LoginUser")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Login should return the same player
	player, err := svc.Login(context.Background(), "firebase-uid-login")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if player.PlayerID != registered.PlayerID {
		t.Errorf("expected PlayerID=%s, got %s", registered.PlayerID, player.PlayerID)
	}
	if player.Username != "LoginUser" {
		t.Errorf("expected Username=LoginUser, got %s", player.Username)
	}
}

func TestLogin_PlayerNotFound(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()

	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	_, err := svc.Login(context.Background(), "nonexistent-uid")
	if err == nil {
		t.Fatal("expected error for nonexistent player")
	}
	if !strings.Contains(err.Error(), "player not found") {
		t.Errorf("expected 'player not found' error, got: %v", err)
	}
}
