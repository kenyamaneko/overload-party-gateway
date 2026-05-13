// Package displaymetacache は対戦相手 / 観戦対象の display meta snapshot を
// 揮発キャッシュとして保持するアダプタ群。
package displaymetacache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// snapshotTTL は Redis に書き込む display meta snapshot の有効期間。
// 試合最長時間 + buffer。試合終了後は明示削除せず TTL 切れで自然消滅させる。
const snapshotTTL = time.Hour

// Hash フィールド名。
const (
	hashFieldName  = "name"
	hashFieldLevel = "level"
)

// RedisStore は Upstash Redis に display meta snapshot を保持するアダプタ。
// `game:{game_id}:player:{player_num}` を Hash key として `{name, level}` を
// 保持する。
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore は RedisStore を生成する。
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

// Put は snapshot を Redis に書き込み、snapshotTTL を設定する。
func (s *RedisStore) Put(ctx context.Context, gameID string, playerNum int, meta port.DisplayMeta) error {
	if err := validateKeyParts(gameID, playerNum); err != nil {
		return err
	}
	key := snapshotKey(gameID, playerNum)
	if err := s.client.HSet(ctx, key,
		hashFieldName, meta.Name,
		hashFieldLevel, meta.Level,
	).Err(); err != nil {
		return fmt.Errorf("redis hset %s: %w", key, err)
	}
	if err := s.client.Expire(ctx, key, snapshotTTL).Err(); err != nil {
		return fmt.Errorf("redis expire %s: %w", key, err)
	}
	return nil
}

// Get は snapshot を Redis から読み出す。key 不在時は port.ErrNotFound を返し、
// 空文字 / level=0 等の暗黙フォールバックは行わない。
func (s *RedisStore) Get(ctx context.Context, gameID string, playerNum int) (port.DisplayMeta, error) {
	if err := validateKeyParts(gameID, playerNum); err != nil {
		return port.DisplayMeta{}, err
	}
	key := snapshotKey(gameID, playerNum)
	fields, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return port.DisplayMeta{}, fmt.Errorf("redis hgetall %s: %w", key, err)
	}
	if len(fields) == 0 {
		return port.DisplayMeta{}, port.ErrNotFound
	}
	name, ok := fields[hashFieldName]
	if !ok {
		return port.DisplayMeta{}, fmt.Errorf("redis hgetall %s: missing field %q", key, hashFieldName)
	}
	levelRaw, ok := fields[hashFieldLevel]
	if !ok {
		return port.DisplayMeta{}, fmt.Errorf("redis hgetall %s: missing field %q", key, hashFieldLevel)
	}
	level, err := strconv.Atoi(levelRaw)
	if err != nil {
		return port.DisplayMeta{}, fmt.Errorf("redis hgetall %s: parse %s %q: %w", key, hashFieldLevel, levelRaw, err)
	}
	return port.DisplayMeta{Name: name, Level: level}, nil
}

// snapshotKey は `game:{game_id}:player:{player_num}` 形式の Redis key を生成する。
func snapshotKey(gameID string, playerNum int) string {
	return fmt.Sprintf("game:%s:player:%d", gameID, playerNum)
}

// validateKeyParts は key 構成要素のガードレール。空 gameID や非正の
// playerNum は呼び出し側のバグなので fail-fast する。
func validateKeyParts(gameID string, playerNum int) error {
	if gameID == "" {
		return errors.New("displaymetacache: gameID is empty")
	}
	if playerNum <= 0 {
		return fmt.Errorf("displaymetacache: playerNum must be positive, got %d", playerNum)
	}
	return nil
}
