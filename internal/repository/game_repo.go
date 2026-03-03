package repository

import (
	"context"

	"github.com/kenyamaneko/overload-party-common/model"
)

// GameRepository defines the data access contract for the game engine.
// The engine depends only on this interface, enabling unit testing with mocks.
type GameRepository interface {
	// CreateGame inserts a new Games row and its initial GameStates row atomically.
	CreateGame(ctx context.Context, game *model.Game, state *model.GameState) error

	// GetGame reads a Games row by game_id.
	GetGame(ctx context.Context, gameID string) (*model.Game, error)

	// GetGameState reads the GameStates row for a game.
	GetGameState(ctx context.Context, gameID string) (*model.GameState, error)

	// UpdateGameState updates GameStates within a read-write transaction.
	// The callback receives the current state; it must modify and return it.
	// The implementation handles optimistic locking (version check + increment).
	UpdateGameState(ctx context.Context, gameID string, fn func(state *model.GameState) error) error

	// AppendEvent inserts a new GameEvents row.
	AppendEvent(ctx context.Context, event *model.GameEvent) error

	// FinishGame updates Games.status to "finished", sets winner_id and finished_at.
	FinishGame(ctx context.Context, gameID, winnerID string) error

	// GetEventCount returns the current max sequence_number for a game.
	GetEventCount(ctx context.Context, gameID string) (int64, error)

	// UpdateGameStatus updates just the Games.status field.
	UpdateGameStatus(ctx context.Context, gameID, status string) error

	// UpdateWinLoss increments wins or losses for a player after a match.
	UpdateWinLoss(ctx context.Context, playerID string, wins, losses int64) error

	// CreateMatch inserts a Matches row.
	CreateMatch(ctx context.Context, match *model.Match) error

	// GetEvents reads all GameEvents for a game, ordered by sequence_number.
	GetEvents(ctx context.Context, gameID string) ([]*model.GameEvent, error)
}
