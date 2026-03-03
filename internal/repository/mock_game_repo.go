package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kenyamaneko/overload-party-common/model"
)

// MockGameRepository is an in-memory implementation of GameRepository for testing.
type MockGameRepository struct {
	mu          sync.Mutex
	games       map[string]*model.Game
	states      map[string]*model.GameState
	events      map[string][]*model.GameEvent
	players     map[string]*mockPlayerData
	matches     []*model.Match
	nextMatchID int64
}

type mockPlayerData struct {
	Wins   int64
	Losses int64
}

// Compile-time interface check.
var _ GameRepository = (*MockGameRepository)(nil)

func NewMockGameRepository() *MockGameRepository {
	return &MockGameRepository{
		games:   make(map[string]*model.Game),
		states:  make(map[string]*model.GameState),
		events:  make(map[string][]*model.GameEvent),
		players: make(map[string]*mockPlayerData),
	}
}

func (r *MockGameRepository) CreateGame(ctx context.Context, game *model.Game, state *model.GameState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.games[game.GameID] = game
	// Deep copy state to avoid aliasing
	stateCopy := *state
	r.states[game.GameID] = &stateCopy
	return nil
}

func (r *MockGameRepository) GetGame(ctx context.Context, gameID string) (*model.Game, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.games[gameID]
	if !ok {
		return nil, fmt.Errorf("game %s not found", gameID)
	}
	return g, nil
}

func (r *MockGameRepository) GetGameState(ctx context.Context, gameID string) (*model.GameState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[gameID]
	if !ok {
		return nil, fmt.Errorf("game state %s not found", gameID)
	}
	return s, nil
}

func (r *MockGameRepository) UpdateGameState(ctx context.Context, gameID string, fn func(state *model.GameState) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.states[gameID]
	if !ok {
		return fmt.Errorf("game state %s not found", gameID)
	}

	if err := fn(s); err != nil {
		return err
	}

	s.Version++
	return nil
}

func (r *MockGameRepository) AppendEvent(ctx context.Context, event *model.GameEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.GameID] = append(r.events[event.GameID], event)
	return nil
}

func (r *MockGameRepository) FinishGame(ctx context.Context, gameID, winnerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.games[gameID]
	if !ok {
		return fmt.Errorf("game %s not found", gameID)
	}
	g.Status = model.GameStatusFinished
	g.WinnerID = &winnerID
	return nil
}

func (r *MockGameRepository) GetEventCount(ctx context.Context, gameID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.events[gameID])), nil
}

func (r *MockGameRepository) UpdateGameStatus(ctx context.Context, gameID, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.games[gameID]
	if !ok {
		return fmt.Errorf("game %s not found", gameID)
	}
	g.Status = status
	return nil
}

func (r *MockGameRepository) UpdateWinLoss(ctx context.Context, playerID string, wins, losses int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.players[playerID]
	if !ok {
		p = &mockPlayerData{}
		r.players[playerID] = p
	}
	p.Wins += wins
	p.Losses += losses
	return nil
}

func (r *MockGameRepository) CreateMatch(ctx context.Context, match *model.Match) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextMatchID++
	match.MatchID = r.nextMatchID
	r.matches = append(r.matches, match)
	return nil
}

// GetEvents returns all events for a game (testing helper).
func (r *MockGameRepository) GetEvents(ctx context.Context, gameID string) ([]*model.GameEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[gameID], nil
}

// --- Test helpers ---

// MustGetState returns the game state or panics.
func (r *MockGameRepository) MustGetState(gameID string) *model.GameState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[gameID]
	if !ok {
		panic(fmt.Sprintf("game state %s not found", gameID))
	}
	return s
}

// InjectState directly sets a game state for testing.
func (r *MockGameRepository) InjectState(gameID string, state *model.GameState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[gameID] = state
}

// InjectGame directly sets a game for testing.
func (r *MockGameRepository) InjectGame(gameID string, game *model.Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.games[gameID] = game
}

// BuildTestField creates a field with JSON marshal for state injection.
func BuildTestField(field *model.Field) json.RawMessage {
	data, _ := json.Marshal(field)
	return data
}

// BuildTestHand creates a hand with JSON marshal for state injection.
func BuildTestHand(hand []model.HandCard) json.RawMessage {
	data, _ := json.Marshal(hand)
	return data
}

// BuildTestRepo creates a repository with JSON marshal for state injection.
func BuildTestRepo(cards []int64) json.RawMessage {
	data, _ := json.Marshal(cards)
	return data
}

// BuildTestTrash creates a trash with JSON marshal for state injection.
func BuildTestTrash(cards []int64) json.RawMessage {
	data, _ := json.Marshal(cards)
	return data
}

// GetWinLoss returns (wins, losses) for a player. Returns (0, 0) if not found.
func (r *MockGameRepository) GetWinLoss(playerID string) (int64, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.players[playerID]
	if !ok {
		return 0, 0
	}
	return p.Wins, p.Losses
}
