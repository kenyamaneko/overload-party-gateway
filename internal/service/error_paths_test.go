package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

var errDB = fmt.Errorf("db connection lost")

// ---------------------------------------------------------------------------
// Error-injecting wrappers for PlayerRepo
// ---------------------------------------------------------------------------

type errorFindByUIDPlayerRepo struct {
	*repository.MockPlayerRepository
}

func (r *errorFindByUIDPlayerRepo) FindByFirebaseUID(_ context.Context, _ string) (*model.Player, error) {
	return nil, errDB
}

type errorFindByIDPlayerRepo struct {
	*repository.MockPlayerRepository
}

func (r *errorFindByIDPlayerRepo) FindByID(_ context.Context, _ string) (*model.Player, error) {
	return nil, errDB
}

type errorCreatePlayerRepo struct {
	*repository.MockPlayerRepository
}

func (r *errorCreatePlayerRepo) Create(_ context.Context, _ *model.Player, _ *model.PlayerDailyBattle) error {
	return errDB
}

type errorGetDailyBattlePlayerRepo struct {
	*repository.MockPlayerRepository
}

func (r *errorGetDailyBattlePlayerRepo) GetDailyBattle(_ context.Context, _ string) (*model.PlayerDailyBattle, error) {
	return nil, errDB
}

// ---------------------------------------------------------------------------
// Error-injecting wrappers for DeckRepo
// ---------------------------------------------------------------------------

type errorGetPlayerCardsDeckRepo struct {
	*mockDeckRepo
}

func (r *errorGetPlayerCardsDeckRepo) GetPlayerCards(_ context.Context, _ string) ([]*model.PlayerCard, error) {
	return nil, errDB
}

type errorCreateDeckRepo struct {
	*mockDeckRepo
}

func (r *errorCreateDeckRepo) Create(_ context.Context, _ *model.Deck, _ []model.DeckCard) error {
	return errDB
}

type errorFindByPlayerIDDeckRepo struct {
	*mockDeckRepo
}

func (r *errorFindByPlayerIDDeckRepo) FindByPlayerID(_ context.Context, _ string) ([]*model.Deck, error) {
	return nil, errDB
}

type errorGetDeckCardsDeckRepo struct {
	*mockDeckRepo
}

func (r *errorGetDeckCardsDeckRepo) GetDeckCards(_ context.Context, _ string, _ int64) ([]model.DeckCard, error) {
	return nil, errDB
}

type errorUpdateDeckRepo struct {
	*mockDeckRepo
}

func (r *errorUpdateDeckRepo) Update(_ context.Context, _ *model.Deck, _ []model.DeckCard) error {
	return errDB
}

// ---------------------------------------------------------------------------
// Error-injecting wrappers for CardRepo
// ---------------------------------------------------------------------------

type errorFindAllCardRepo struct {
	*repository.MockCardRepository
}

func (r *errorFindAllCardRepo) FindAll(_ context.Context) ([]*model.CardDefinition, error) {
	return nil, errDB
}

type errorGetPlayerCardsDeckRepoForCard struct {
	*repository.MockDeckRepository
}

func (r *errorGetPlayerCardsDeckRepoForCard) GetPlayerCards(_ context.Context, _ string) ([]*model.PlayerCard, error) {
	return nil, errDB
}

// ---------------------------------------------------------------------------
// Error-injecting wrapper for GameConfigRepo
// ---------------------------------------------------------------------------

type errorGameConfigRepo struct{}

func (r *errorGameConfigRepo) GetInt64(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, errDB
}

// ---------------------------------------------------------------------------
// Error-injecting wrappers for UserSettingsRepo
// ---------------------------------------------------------------------------

type errorUpsertSettingsRepo struct {
	*repository.MockUserSettingsRepository
}

func (r *errorUpsertSettingsRepo) Upsert(_ context.Context, _ *model.UserSettings) error {
	return errDB
}

// ===========================================================================
// AuthService error path tests
// ===========================================================================

func TestRegister_FindByFirebaseUID_Error(t *testing.T) {
	playerRepo := &errorFindByUIDPlayerRepo{repository.NewMockPlayerRepository()}
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	_, err := svc.Register(context.Background(), "uid1", "User")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check existing player")
}

func TestRegister_Create_Error(t *testing.T) {
	playerRepo := &errorCreatePlayerRepo{repository.NewMockPlayerRepository()}
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	_, err := svc.Register(context.Background(), "uid1", "User")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create player")
}

func TestRegister_SettingsUpsert_Error_NonFatal(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := newStampTrackingShopRepo()
	userSettingsRepo := &errorUpsertSettingsRepo{repository.NewMockUserSettingsRepository()}
	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	player, err := svc.Register(context.Background(), "uid1", "User")
	require.NoError(t, err, "settings upsert failure should not be fatal")
	assert.NotEmpty(t, player.PlayerID)
}

func TestLogin_FindByFirebaseUID_Error(t *testing.T) {
	playerRepo := &errorFindByUIDPlayerRepo{repository.NewMockPlayerRepository()}
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	svc := NewAuthService(playerRepo, shopRepo, userSettingsRepo)

	_, err := svc.Login(context.Background(), "uid1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find player")
}

// ===========================================================================
// DeckService error path tests
// ===========================================================================

func TestCreateDeck_GetPlayerCards_Error(t *testing.T) {
	repo := &errorGetPlayerCardsDeckRepo{newMockDeckRepo()}
	cc := cache.NewCardCache()
	svc := NewDeckService(repo, cc)

	_, err := svc.CreateDeck(context.Background(), "p1", CreateDeckRequest{
		DeckName: "Test", Cards: makeEntries(1, 3),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get owned cards")
}

func TestCreateDeck_RepoCreate_Error(t *testing.T) {
	baseRepo := newMockDeckRepo()
	repo := &errorCreateDeckRepo{baseRepo}
	_, _, cc := setupDeckService()
	svc := NewDeckService(repo, cc)

	// Grant cards so validation passes
	grantUnlimited(baseRepo, "p1", 1)

	_, err := svc.CreateDeck(context.Background(), "p1", CreateDeckRequest{
		DeckName: "Test", Cards: makeEntries(1, 3),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create deck")
}

func TestGetDecks_FindByPlayerID_Error(t *testing.T) {
	repo := &errorFindByPlayerIDDeckRepo{newMockDeckRepo()}
	cc := cache.NewCardCache()
	svc := NewDeckService(repo, cc)

	_, err := svc.GetDecks(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db connection lost")
}

func TestGetDeck_GetDeckCards_Error(t *testing.T) {
	baseRepo := newMockDeckRepo()
	// Create a deck first, then wrap with error repo
	cc := cache.NewCardCache()
	cc.InjectForTest(1, &model.CardDefinition{CardNo: 1, Restriction: "unlimited", IsActive: true})
	grantUnlimited(baseRepo, "p1", 1)
	svc := NewDeckService(baseRepo, cc)
	created, err := svc.CreateDeck(context.Background(), "p1", CreateDeckRequest{
		DeckName: "Test", Cards: makeEntries(1, 3),
	})
	require.NoError(t, err)

	// Now wrap with error repo for GetDeckCards
	errRepo := &errorGetDeckCardsDeckRepo{baseRepo}
	svc2 := NewDeckService(errRepo, cc)

	_, _, err = svc2.GetDeck(context.Background(), "p1", created.DeckID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get deck cards")
}

func TestUpdateDeck_GetPlayerCards_Error(t *testing.T) {
	repo := &errorGetPlayerCardsDeckRepo{newMockDeckRepo()}
	cc := cache.NewCardCache()
	svc := NewDeckService(repo, cc)

	_, err := svc.UpdateDeck(context.Background(), "p1", 1, UpdateDeckRequest{
		DeckName: "Test", Cards: makeEntries(1, 3),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get owned cards")
}

func TestUpdateDeck_RepoUpdate_Error(t *testing.T) {
	baseRepo := newMockDeckRepo()
	_, _, cc := setupDeckService()
	svc := NewDeckService(baseRepo, cc)
	grantUnlimited(baseRepo, "p1", 1)

	created, err := svc.CreateDeck(context.Background(), "p1", CreateDeckRequest{
		DeckName: "Test", Cards: makeEntries(1, 3),
	})
	require.NoError(t, err)

	errRepo := &errorUpdateDeckRepo{baseRepo}
	svc2 := NewDeckService(errRepo, cc)

	_, err = svc2.UpdateDeck(context.Background(), "p1", created.DeckID, UpdateDeckRequest{
		DeckName: "Updated", Cards: makeEntries(1, 3),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update deck")
}

func TestGetPlayerCards_Error(t *testing.T) {
	repo := &errorGetPlayerCardsDeckRepo{newMockDeckRepo()}
	cc := cache.NewCardCache()
	svc := NewDeckService(repo, cc)

	_, err := svc.GetPlayerCards(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db connection lost")
}

// ===========================================================================
// PlayerService error path tests
// ===========================================================================

func TestGetPlayer_FindByID_Error(t *testing.T) {
	playerRepo := &errorFindByIDPlayerRepo{repository.NewMockPlayerRepository()}
	configRepo := &mockGameConfigRepo{values: map[string]int64{configKeyFreeDailyBattleLimit: 10}}
	svc := NewPlayerService(playerRepo, configRepo)

	_, err := svc.GetPlayer(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find player")
}

func TestGetBattleLimit_FindByID_Error(t *testing.T) {
	playerRepo := &errorFindByIDPlayerRepo{repository.NewMockPlayerRepository()}
	configRepo := &mockGameConfigRepo{values: map[string]int64{configKeyFreeDailyBattleLimit: 10}}
	svc := NewPlayerService(playerRepo, configRepo)

	_, err := svc.GetBattleLimit(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find player")
}

func TestGetBattleLimit_GetDailyBattle_Error(t *testing.T) {
	baseRepo := repository.NewMockPlayerRepository()
	_ = baseRepo.Create(context.Background(), &model.Player{
		PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: civil.DateOf(time.Now())})

	errRepo := &errorGetDailyBattlePlayerRepo{baseRepo}
	configRepo := &mockGameConfigRepo{values: map[string]int64{configKeyFreeDailyBattleLimit: 10}}
	svc := NewPlayerService(errRepo, configRepo)

	_, err := svc.GetBattleLimit(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get daily battle")
}

func TestGetBattleLimit_GetGameConfig_Error(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	_ = playerRepo.Create(context.Background(), &model.Player{
		PlayerID: "p1", FirebaseUID: "uid1", IsPremium: false,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, &model.PlayerDailyBattle{PlayerID: "p1", DailyBattleCount: 0, LastResetDate: today()})

	configRepo := &errorGameConfigRepo{}
	svc := NewPlayerService(playerRepo, configRepo)

	_, err := svc.GetBattleLimit(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get free battle limit")
}

// ===========================================================================
// CardService error path tests
// ===========================================================================

func TestGetAllCards_FindAll_Error(t *testing.T) {
	cardRepo := &errorFindAllCardRepo{repository.NewMockCardRepository(nil)}
	deckRepo := repository.NewMockDeckRepository()
	svc := NewCardService(cardRepo, deckRepo)

	_, err := svc.GetAllCards(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get all cards")
}

func TestGetAllCards_GetPlayerCards_Error(t *testing.T) {
	cardRepo := repository.NewMockCardRepository(nil)
	deckRepo := &errorGetPlayerCardsDeckRepoForCard{repository.NewMockDeckRepository()}
	svc := NewCardService(cardRepo, deckRepo)

	_, err := svc.GetAllCards(context.Background(), "p1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get player cards")
}
