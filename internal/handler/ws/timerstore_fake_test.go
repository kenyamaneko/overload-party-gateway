package ws

import (
	"context"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// disconnectDeadlineCall は fakeTimerStore.SetDisconnectDeadline への 1 回の呼出を記録する。
type disconnectDeadlineCall struct {
	playerID string
	gameID   string
	deadline time.Time
}

// turnDeadlineCall は fakeTimerStore.SetTurnDeadline への 1 回の呼出を記録する。
type turnDeadlineCall struct {
	gameID         string
	activePlayerID string
	deadline       time.Time
}

// fakeTimerStore は port.TimerStore のテスト用実装。呼び出し引数を記録し、
// エラー注入で書き込み失敗時に握りつぶし（警告ログのみ）が働くことを検証できるようにする。
type fakeTimerStore struct {
	mu sync.Mutex

	setDisconnectCalls   []disconnectDeadlineCall
	setDisconnectErr     error
	clearDisconnectCalls []string
	clearDisconnectErr   error

	setTurnCalls   []turnDeadlineCall
	setTurnErr     error
	clearTurnCalls []string
	clearTurnErr   error
}

func (f *fakeTimerStore) SetDisconnectDeadline(_ context.Context, playerID, gameID string, deadline time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setDisconnectCalls = append(f.setDisconnectCalls, disconnectDeadlineCall{playerID, gameID, deadline})
	return f.setDisconnectErr
}

func (f *fakeTimerStore) ClearDisconnectDeadline(_ context.Context, playerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearDisconnectCalls = append(f.clearDisconnectCalls, playerID)
	return f.clearDisconnectErr
}

func (f *fakeTimerStore) GetDisconnectDeadline(_ context.Context, _ string) (port.DisconnectDeadline, bool, error) {
	return port.DisconnectDeadline{}, false, nil
}

func (f *fakeTimerStore) SetTurnDeadline(_ context.Context, gameID, activePlayerID string, deadline time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setTurnCalls = append(f.setTurnCalls, turnDeadlineCall{gameID, activePlayerID, deadline})
	return f.setTurnErr
}

func (f *fakeTimerStore) ClearTurnDeadline(_ context.Context, gameID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearTurnCalls = append(f.clearTurnCalls, gameID)
	return f.clearTurnErr
}

func (f *fakeTimerStore) GetTurnDeadline(_ context.Context, _ string) (port.TurnDeadline, bool, error) {
	return port.TurnDeadline{}, false, nil
}

func (f *fakeTimerStore) snapshotSetDisconnectCalls() []disconnectDeadlineCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]disconnectDeadlineCall, len(f.setDisconnectCalls))
	copy(out, f.setDisconnectCalls)
	return out
}

func (f *fakeTimerStore) snapshotClearDisconnectCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.clearDisconnectCalls))
	copy(out, f.clearDisconnectCalls)
	return out
}

func (f *fakeTimerStore) snapshotSetTurnCalls() []turnDeadlineCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]turnDeadlineCall, len(f.setTurnCalls))
	copy(out, f.setTurnCalls)
	return out
}

func (f *fakeTimerStore) snapshotClearTurnCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.clearTurnCalls))
	copy(out, f.clearTurnCalls)
	return out
}

var _ port.TimerStore = (*fakeTimerStore)(nil)
