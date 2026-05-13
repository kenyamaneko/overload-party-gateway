package displaymetacache

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// MemoryStore は pod-local in-memory な display meta snapshot 保持。
// 2 段キャッシュの L1 として用いる。
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]port.DisplayMeta
}

// NewMemoryStore は MemoryStore を生成する。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]port.DisplayMeta)}
}

// Put は in-memory map に snapshot を書き込む。
func (s *MemoryStore) Put(_ context.Context, gameID string, playerNum int, meta port.DisplayMeta) error {
	if err := validateKeyParts(gameID, playerNum); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[snapshotKey(gameID, playerNum)] = meta
	return nil
}

// Get は in-memory map から snapshot を読み出す。存在しない場合は
// port.ErrNotFound を返す。
func (s *MemoryStore) Get(_ context.Context, gameID string, playerNum int) (port.DisplayMeta, error) {
	if err := validateKeyParts(gameID, playerNum); err != nil {
		return port.DisplayMeta{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.entries[snapshotKey(gameID, playerNum)]
	if !ok {
		return port.DisplayMeta{}, port.ErrNotFound
	}
	return meta, nil
}

// Evict は指定 key の snapshot を削除する。2 段キャッシュの L2 書き込み
// 失敗時に L1 を巻き戻す経路で使う。未書き込み key を Evict しても error
// にはしない (再 Evict が冪等)。
func (s *MemoryStore) Evict(_ context.Context, gameID string, playerNum int) error {
	if err := validateKeyParts(gameID, playerNum); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, snapshotKey(gameID, playerNum))
	return nil
}
