package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-common/model"
)

// PgGameRepository implements GameRepository using PostgreSQL via pgxpool.
type PgGameRepository struct {
	pool *pgxpool.Pool
}

// NewPgGameRepository creates a new PgGameRepository.
func NewPgGameRepository(pool *pgxpool.Pool) *PgGameRepository {
	return &PgGameRepository{pool: pool}
}

// Compile-time interface check.
var _ GameRepository = (*PgGameRepository)(nil)

func (r *PgGameRepository) CreateGame(ctx context.Context, game *model.Game, state *model.GameState) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin txn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO games (
			game_id, player1_id, player2_id,
			player1_deck_snapshot, player2_deck_snapshot,
			status, winner_id,
			created_at, updated_at, finished_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		game.GameID, game.Player1ID, game.Player2ID,
		game.Player1DeckSnapshot, game.Player2DeckSnapshot,
		game.Status, game.WinnerID,
		game.CreatedAt, game.UpdatedAt, game.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("insert game: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO game_states (
			game_id, version, current_turn, current_phase, active_player,
			player1_budget, player1_insight_pool, player1_field, player1_hand,
			player1_repository, player1_trash, player1_time_bank,
			player2_budget, player2_insight_pool, player2_field, player2_hand,
			player2_repository, player2_trash, player2_time_bank,
			chain_stack, current_action_timer, next_instance_seq, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		state.GameID, state.Version, state.CurrentTurn, state.CurrentPhase, state.ActivePlayer,
		state.Player1Budget, state.Player1InsightPool, state.Player1Field, state.Player1Hand,
		state.Player1Repository, state.Player1Trash, state.Player1TimeBank,
		state.Player2Budget, state.Player2InsightPool, state.Player2Field, state.Player2Hand,
		state.Player2Repository, state.Player2Trash, state.Player2TimeBank,
		state.ChainStack, state.CurrentActionTimer, state.NextInstanceSeq, state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert game state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create game: %w", err)
	}
	return nil
}

func (r *PgGameRepository) GetGame(ctx context.Context, gameID string) (*model.Game, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT game_id, player1_id, player2_id,
		       player1_deck_snapshot, player2_deck_snapshot,
		       status, winner_id,
		       created_at, updated_at, finished_at
		FROM games WHERE game_id = $1`, gameID)

	var g model.Game
	err := row.Scan(
		&g.GameID, &g.Player1ID, &g.Player2ID,
		&g.Player1DeckSnapshot, &g.Player2DeckSnapshot,
		&g.Status, &g.WinnerID,
		&g.CreatedAt, &g.UpdatedAt, &g.FinishedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("game not found: %s", gameID)
		}
		return nil, fmt.Errorf("scan game: %w", err)
	}
	return &g, nil
}

func (r *PgGameRepository) GetGameState(ctx context.Context, gameID string) (*model.GameState, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT game_id, version, current_turn, current_phase, active_player,
		       player1_budget, player1_insight_pool, player1_field, player1_hand,
		       player1_repository, player1_trash, player1_time_bank,
		       player2_budget, player2_insight_pool, player2_field, player2_hand,
		       player2_repository, player2_trash, player2_time_bank,
		       chain_stack, current_action_timer, next_instance_seq, updated_at
		FROM game_states WHERE game_id = $1`, gameID)

	var state model.GameState
	err := row.Scan(
		&state.GameID, &state.Version, &state.CurrentTurn, &state.CurrentPhase, &state.ActivePlayer,
		&state.Player1Budget, &state.Player1InsightPool, &state.Player1Field, &state.Player1Hand,
		&state.Player1Repository, &state.Player1Trash, &state.Player1TimeBank,
		&state.Player2Budget, &state.Player2InsightPool, &state.Player2Field, &state.Player2Hand,
		&state.Player2Repository, &state.Player2Trash, &state.Player2TimeBank,
		&state.ChainStack, &state.CurrentActionTimer, &state.NextInstanceSeq, &state.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("game state not found: %s", gameID)
		}
		return nil, fmt.Errorf("scan game state: %w", err)
	}
	return &state, nil
}

func (r *PgGameRepository) UpdateGameState(ctx context.Context, gameID string, fn func(state *model.GameState) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin txn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// SELECT FOR UPDATE to lock the row within the transaction.
	row := tx.QueryRow(ctx, `
		SELECT game_id, version, current_turn, current_phase, active_player,
		       player1_budget, player1_insight_pool, player1_field, player1_hand,
		       player1_repository, player1_trash, player1_time_bank,
		       player2_budget, player2_insight_pool, player2_field, player2_hand,
		       player2_repository, player2_trash, player2_time_bank,
		       chain_stack, current_action_timer, next_instance_seq, updated_at
		FROM game_states WHERE game_id = $1 FOR UPDATE`, gameID)

	var state model.GameState
	err = row.Scan(
		&state.GameID, &state.Version, &state.CurrentTurn, &state.CurrentPhase, &state.ActivePlayer,
		&state.Player1Budget, &state.Player1InsightPool, &state.Player1Field, &state.Player1Hand,
		&state.Player1Repository, &state.Player1Trash, &state.Player1TimeBank,
		&state.Player2Budget, &state.Player2InsightPool, &state.Player2Field, &state.Player2Hand,
		&state.Player2Repository, &state.Player2Trash, &state.Player2TimeBank,
		&state.ChainStack, &state.CurrentActionTimer, &state.NextInstanceSeq, &state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("read game state in txn: %w", err)
	}

	if err := fn(&state); err != nil {
		return err
	}

	state.Version++
	state.UpdatedAt = time.Now()

	_, err = tx.Exec(ctx, `
		UPDATE game_states SET
			version = $1, current_turn = $2, current_phase = $3, active_player = $4,
			player1_budget = $5, player1_insight_pool = $6, player1_field = $7, player1_hand = $8,
			player1_repository = $9, player1_trash = $10, player1_time_bank = $11,
			player2_budget = $12, player2_insight_pool = $13, player2_field = $14, player2_hand = $15,
			player2_repository = $16, player2_trash = $17, player2_time_bank = $18,
			chain_stack = $19, current_action_timer = $20, next_instance_seq = $21, updated_at = $22
		WHERE game_id = $23`,
		state.Version, state.CurrentTurn, state.CurrentPhase, state.ActivePlayer,
		state.Player1Budget, state.Player1InsightPool, state.Player1Field, state.Player1Hand,
		state.Player1Repository, state.Player1Trash, state.Player1TimeBank,
		state.Player2Budget, state.Player2InsightPool, state.Player2Field, state.Player2Hand,
		state.Player2Repository, state.Player2Trash, state.Player2TimeBank,
		state.ChainStack, state.CurrentActionTimer, state.NextInstanceSeq, state.UpdatedAt,
		state.GameID,
	)
	if err != nil {
		return fmt.Errorf("update game state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update game state: %w", err)
	}
	return nil
}

func (r *PgGameRepository) AppendEvent(ctx context.Context, event *model.GameEvent) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO game_events (
			game_id, sequence_number, event_type, player_id, event_data, created_at
		) VALUES ($1,$2,$3,$4,$5,$6)`,
		event.GameID, event.SequenceNumber, event.EventType,
		event.PlayerID, event.EventData, event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (r *PgGameRepository) FinishGame(ctx context.Context, gameID, winnerID string) error {
	now := time.Now()
	_, err := r.pool.Exec(ctx, `
		UPDATE games SET status = $1, winner_id = $2, finished_at = $3, updated_at = $4
		WHERE game_id = $5`,
		model.GameStatusFinished, winnerID, now, now, gameID,
	)
	if err != nil {
		return fmt.Errorf("finish game: %w", err)
	}
	return nil
}

func (r *PgGameRepository) GetEventCount(ctx context.Context, gameID string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM game_events WHERE game_id = $1", gameID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return count, nil
}

func (r *PgGameRepository) UpdateGameStatus(ctx context.Context, gameID, status string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE games SET status = $1, updated_at = $2 WHERE game_id = $3`,
		status, time.Now(), gameID,
	)
	if err != nil {
		return fmt.Errorf("update game status: %w", err)
	}
	return nil
}

func (r *PgGameRepository) UpdateWinLoss(ctx context.Context, playerID string, wins, losses int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE players SET wins = $1, losses = $2, updated_at = $3 WHERE player_id = $4`,
		wins, losses, time.Now(), playerID,
	)
	if err != nil {
		return fmt.Errorf("update win/loss: %w", err)
	}
	return nil
}

func (r *PgGameRepository) CreateMatch(ctx context.Context, match *model.Match) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO matches (game_id, created_at)
		VALUES ($1,$2) RETURNING match_id`,
		match.GameID, match.CreatedAt,
	).Scan(&match.MatchID)
	if err != nil {
		return fmt.Errorf("create match: %w", err)
	}
	return nil
}

func (r *PgGameRepository) GetEvents(ctx context.Context, gameID string) ([]*model.GameEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT game_id, sequence_number, event_type, player_id, event_data, created_at
		FROM game_events
		WHERE game_id = $1
		ORDER BY sequence_number`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []*model.GameEvent
	for rows.Next() {
		var e model.GameEvent
		if err := rows.Scan(
			&e.GameID, &e.SequenceNumber, &e.EventType,
			&e.PlayerID, &e.EventData, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}
