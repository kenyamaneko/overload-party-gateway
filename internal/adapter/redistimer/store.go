package redistimer

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

const (
	disconnectKeyPrefix = "gateway:timer:disconnect:"
	turnKeyPrefix       = "gateway:timer:turn:"

	fieldGameID         = "game_id"
	fieldActivePlayerID = "active_player_id"
	fieldDeadlineMillis = "deadline_unix_ms"

	// strayTTL は明示的な削除が行われなかったキーを取り残さないための安全網。
	// 対戦の実耐用時間を十分に超える値とし、通常運用では明示削除が先に効く。
	strayTTL = 24 * time.Hour
)

var _ port.TimerStore = (*Store)(nil)

// Store は port.TimerStore を Redis の Hash で実装する。
type Store struct {
	client *redis.Client
}

// NewStore は接続済みの Redis client から Store を生成する。
func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

// SetDisconnectDeadline はプレイヤーの切断猶予期限を書き込む。
func (s *Store) SetDisconnectDeadline(ctx context.Context, playerID, gameID string, deadline time.Time) error {
	key := disconnectKeyPrefix + playerID
	if err := s.client.HSet(ctx, key, map[string]any{
		fieldGameID:         gameID,
		fieldDeadlineMillis: deadline.UnixMilli(),
	}).Err(); err != nil {
		return fmt.Errorf("redistimer: set disconnect deadline: %w", err)
	}
	if err := s.client.Expire(ctx, key, strayTTL).Err(); err != nil {
		return fmt.Errorf("redistimer: expire disconnect deadline: %w", err)
	}
	return nil
}

// ClearDisconnectDeadline はプレイヤーの切断猶予期限を削除する。
func (s *Store) ClearDisconnectDeadline(ctx context.Context, playerID string) error {
	if err := s.client.Del(ctx, disconnectKeyPrefix+playerID).Err(); err != nil {
		return fmt.Errorf("redistimer: clear disconnect deadline: %w", err)
	}
	return nil
}

// GetDisconnectDeadline はプレイヤーの切断猶予期限を読み出す。
func (s *Store) GetDisconnectDeadline(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
	fields, err := s.client.HGetAll(ctx, disconnectKeyPrefix+playerID).Result()
	if err != nil {
		return port.DisconnectDeadline{}, false, fmt.Errorf("redistimer: get disconnect deadline: %w", err)
	}
	if len(fields) == 0 {
		return port.DisconnectDeadline{}, false, nil
	}
	deadline, err := parseDeadline(fields[fieldDeadlineMillis])
	if err != nil {
		return port.DisconnectDeadline{}, false, fmt.Errorf("redistimer: parse disconnect deadline: %w", err)
	}
	return port.DisconnectDeadline{
		GameID:   fields[fieldGameID],
		Deadline: deadline,
	}, true, nil
}

// SetTurnDeadline はゲームの現在のターンの期限を書き込む。
func (s *Store) SetTurnDeadline(ctx context.Context, gameID, activePlayerID string, deadline time.Time) error {
	key := turnKeyPrefix + gameID
	if err := s.client.HSet(ctx, key, map[string]any{
		fieldActivePlayerID: activePlayerID,
		fieldDeadlineMillis: deadline.UnixMilli(),
	}).Err(); err != nil {
		return fmt.Errorf("redistimer: set turn deadline: %w", err)
	}
	if err := s.client.Expire(ctx, key, strayTTL).Err(); err != nil {
		return fmt.Errorf("redistimer: expire turn deadline: %w", err)
	}
	return nil
}

// ClearTurnDeadline はゲームのターン期限を削除する。
func (s *Store) ClearTurnDeadline(ctx context.Context, gameID string) error {
	if err := s.client.Del(ctx, turnKeyPrefix+gameID).Err(); err != nil {
		return fmt.Errorf("redistimer: clear turn deadline: %w", err)
	}
	return nil
}

// GetTurnDeadline はゲームの現在のターンの期限を読み出す。
func (s *Store) GetTurnDeadline(ctx context.Context, gameID string) (port.TurnDeadline, bool, error) {
	fields, err := s.client.HGetAll(ctx, turnKeyPrefix+gameID).Result()
	if err != nil {
		return port.TurnDeadline{}, false, fmt.Errorf("redistimer: get turn deadline: %w", err)
	}
	if len(fields) == 0 {
		return port.TurnDeadline{}, false, nil
	}
	deadline, err := parseDeadline(fields[fieldDeadlineMillis])
	if err != nil {
		return port.TurnDeadline{}, false, fmt.Errorf("redistimer: parse turn deadline: %w", err)
	}
	return port.TurnDeadline{
		ActivePlayerID: fields[fieldActivePlayerID],
		Deadline:       deadline,
	}, true, nil
}

func parseDeadline(raw string) (time.Time, error) {
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}
