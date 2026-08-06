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

// fakeTimerStore は port.TimerStore のテスト用実装。呼び出し引数を記録し、
// エラー注入で書き込み失敗時に握りつぶし（警告ログのみ）が働くことを検証できるようにする。
type fakeTimerStore struct {
	mu sync.Mutex

	setDisconnectCalls   []disconnectDeadlineCall
	setDisconnectErr     error
	clearDisconnectCalls []string
	clearDisconnectErr   error

	// getDisconnectReturn は GetDisconnectDeadline の固定応答。playerID によらず
	// 同じ値を返す簡易スタブ (テストは呼出ごとに 1 プレイヤーだけを問い合わせる)。
	getDisconnectReturn port.DisconnectDeadline
	getDisconnectFound  bool
	getDisconnectErr    error
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
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getDisconnectReturn, f.getDisconnectFound, f.getDisconnectErr
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

// portDisconnectDeadline は fakeTimerStore.getDisconnectReturn を組み立てる。
func portDisconnectDeadline(gameID string, deadline time.Time) port.DisconnectDeadline {
	return port.DisconnectDeadline{GameID: gameID, Deadline: deadline}
}

var _ port.TimerStore = (*fakeTimerStore)(nil)
