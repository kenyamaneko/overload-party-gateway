package ws

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// fakeInvalidatedGameRepo は port.InvalidatedGameRepo のテスト用実装。
// 記録を保持するため、停止時に書いた内容を起動時の後始末がそのまま読み出せる。
type fakeInvalidatedGameRepo struct {
	mu sync.Mutex

	// order は無効として記録された順の対戦 ID。
	order []string
	// finished は対戦 ID から battle 上で決着済みかへの対応。
	finished map[string]bool

	markInvalidatedErr error
	listUnfinishedErr  error
	markFinishedErr    error
	isInvalidatedErr   error
}

func newFakeInvalidatedGameRepo() *fakeInvalidatedGameRepo {
	return &fakeInvalidatedGameRepo{finished: make(map[string]bool)}
}

func (f *fakeInvalidatedGameRepo) MarkInvalidated(_ context.Context, gameIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markInvalidatedErr != nil {
		return f.markInvalidatedErr
	}
	for _, gameID := range gameIDs {
		if _, ok := f.finished[gameID]; ok {
			continue
		}
		f.finished[gameID] = false
		f.order = append(f.order, gameID)
	}
	return nil
}

func (f *fakeInvalidatedGameRepo) ListUnfinished(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listUnfinishedErr != nil {
		return nil, f.listUnfinishedErr
	}
	var unfinished []string
	for _, gameID := range f.order {
		if !f.finished[gameID] {
			unfinished = append(unfinished, gameID)
		}
	}
	return unfinished, nil
}

func (f *fakeInvalidatedGameRepo) MarkFinished(_ context.Context, gameID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markFinishedErr != nil {
		return f.markFinishedErr
	}
	f.finished[gameID] = true
	return nil
}

func (f *fakeInvalidatedGameRepo) IsInvalidated(_ context.Context, gameID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isInvalidatedErr != nil {
		return false, f.isInvalidatedErr
	}
	_, ok := f.finished[gameID]
	return ok, nil
}

// snapshotInvalidated は無効として記録された対戦 ID を記録順に返す。
func (f *fakeInvalidatedGameRepo) snapshotInvalidated() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

var _ port.InvalidatedGameRepo = (*fakeInvalidatedGameRepo)(nil)
