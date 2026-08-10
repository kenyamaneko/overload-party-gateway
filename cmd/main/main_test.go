package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
)

// captureLoggedLine は newCloudLoggingHandler が実際に書き込む1行分のログを標準出力から
// 捕捉し、JSON としてデコードして返す。Writer が os.Stdout に固定された関数なので、
// os.Stdout を差し替えたうえで出力をパイプ経由で読み戻す。
func captureLoggedLine(t *testing.T, level slog.Level, message string) map[string]any {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	logger := slog.New(newCloudLoggingHandler())
	logger.Log(context.Background(), level, message)
	require.NoError(t, w.Close())

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func TestNewCloudLoggingHandler(t *testing.T) {
	t.Run("ログレベルからCloud Logging severityへの変換", func(t *testing.T) {
		cases := []struct {
			name         string
			level        slog.Level
			wantSeverity string
		}{
			{
				name:         "ログレベルが0(Info)のとき、severityはINFOになる",
				level:        slog.LevelInfo,
				wantSeverity: "INFO",
			},
			{
				name:         "ログレベルが3(Warnの下限未満)のとき、severityはINFOになる",
				level:        slog.Level(3),
				wantSeverity: "INFO",
			},
			{
				name:         "ログレベルが4(Warn)のとき、severityはWARNINGになる",
				level:        slog.LevelWarn,
				wantSeverity: "WARNING",
			},
			{
				name:         "ログレベルが7(Errorの下限未満)のとき、severityはWARNINGになる",
				level:        slog.Level(7),
				wantSeverity: "WARNING",
			},
			{
				name:         "ログレベルが8(Error)のとき、severityはERRORになる",
				level:        slog.LevelError,
				wantSeverity: "ERROR",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out := captureLoggedLine(t, tc.level, "test message")
				assert.Equal(t, tc.wantSeverity, out["severity"])
			})
		}
	})

	t.Run("メッセージキーのCloud Logging互換名への変換", func(t *testing.T) {
		t.Run("ログ出力時、メッセージがmessageキーで記録される", func(t *testing.T) {
			out := captureLoggedLine(t, slog.LevelInfo, "hello world")
			assert.Equal(t, "hello world", out["message"])
			assert.NotContains(t, out, "msg")
		})
	})
}

// fakeWSLifecycle は wsLifecycle の呼び出し内容 (呼ばれたか・渡された ctx) を記録するフェイク。
type fakeWSLifecycle struct {
	mu          sync.Mutex
	shutdownCtx context.Context
	recoverCtx  context.Context
}

func (f *fakeWSLifecycle) Shutdown(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCtx = ctx
}

func (f *fakeWSLifecycle) RecoverInvalidatedGames(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recoverCtx = ctx
}

func (f *fakeWSLifecycle) calledShutdown() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdownCtx
}

func (f *fakeWSLifecycle) calledRecover() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recoverCtx
}

// runServicesWithTimeout は runServices を呼び出し、テスト対象の不具合で呼び出しが
// 返らなくなってもテストスイート全体を止めないよう、上限時間を超えたら Fatal で打ち切る。
func runServicesWithTimeout(t *testing.T, ctx context.Context, cfg *config.Config, srv *http.Server, wsRuntime wsLifecycle) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServices(ctx, cfg, srv, wsRuntime)
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("runServices did not return within the timeout")
		return nil
	}
}

// runServicesAfterListening は HTTP サーバが実際に接続を受け付け始めるまで待ってから
// ctx をキャンセルし、runServices の戻り値と wsLifecycle 呼び出し内容を返す。
// あらかじめキャンセル済みの ctx を渡すと http.Server.ListenAndServe が listen 自体を
// 行わずに即終了してしまい、「起動してから正常に閉じた」ことを確かめられないため、
// BaseContext (listener が確立してから呼ばれる) を合図に使う。
func runServicesAfterListening(t *testing.T) (error, *fakeWSLifecycle) {
	t.Helper()

	fake := &fakeWSLifecycle{}
	listening := make(chan struct{}, 1)
	srv := &http.Server{Addr: "127.0.0.1:0"}
	srv.BaseContext = func(net.Listener) context.Context {
		listening <- struct{}{}
		return context.Background()
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServices(ctx, newTestConfig(), srv, fake)
	}()

	select {
	case <-listening:
	case <-time.After(5 * time.Second):
		t.Fatal("http server did not start listening within the timeout")
	}
	cancel()

	select {
	case err := <-errCh:
		return err, fake
	case <-time.After(5 * time.Second):
		t.Fatal("runServices did not return within the timeout")
		return nil, fake
	}
}

// assertDeadlineWithin は ctx に設定された上限時間が、呼び出し時点から want だけ先に
// あることを確かめる(許容誤差1秒)。
func assertDeadlineWithin(t *testing.T, ctx context.Context, want time.Duration) {
	t.Helper()
	require.NotNil(t, ctx)
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	remaining := time.Until(deadline)
	assert.InDelta(t, want.Seconds(), remaining.Seconds(), 1.0)
}

func newTestConfig() *config.Config {
	return &config.Config{Port: "0", Env: "test"}
}

func TestRunServices(t *testing.T) {
	t.Run("シャットダウンシグナル受信時の制御", func(t *testing.T) {
		t.Run("HTTPサーバが接続を受け付けた後にctxがキャンセルされると、エラーなく終了する", func(t *testing.T) {
			err, _ := runServicesAfterListening(t)

			assert.NoError(t, err)
		})

		t.Run("対戦の復旧処理には、60秒の上限時間が設定される", func(t *testing.T) {
			_, fake := runServicesAfterListening(t)

			assertDeadlineWithin(t, fake.calledRecover(), 60*time.Second)
		})

		t.Run("WS接続への終了通知には、5秒の上限時間が設定される", func(t *testing.T) {
			_, fake := runServicesAfterListening(t)

			assertDeadlineWithin(t, fake.calledShutdown(), 5*time.Second)
		})
	})

	t.Run("HTTPサーバの起動に失敗したときの制御", func(t *testing.T) {
		// ポート番号を欠いた不正なアドレスにより、net.Listen が即座にエラーを返す。
		invalidAddr := "missing-port"

		t.Run("アドレスが不正でリスンに失敗すると、httpサーバ起動由来のエラーを返す", func(t *testing.T) {
			srv := &http.Server{Addr: invalidAddr}
			fake := &fakeWSLifecycle{}

			err := runServicesWithTimeout(t, context.Background(), newTestConfig(), srv, fake)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "http server:")
			assert.ErrorContains(t, err, "missing port")
		})

		t.Run("アドレスが不正でリスンに失敗しても、WS接続へ終了を通知する", func(t *testing.T) {
			srv := &http.Server{Addr: invalidAddr}
			fake := &fakeWSLifecycle{}

			_ = runServicesWithTimeout(t, context.Background(), newTestConfig(), srv, fake)

			assert.NotNil(t, fake.calledShutdown())
		})

		t.Run("アドレスが不正でリスンに失敗しても、対戦の復旧処理を実行する", func(t *testing.T) {
			srv := &http.Server{Addr: invalidAddr}
			fake := &fakeWSLifecycle{}

			_ = runServicesWithTimeout(t, context.Background(), newTestConfig(), srv, fake)

			assert.NotNil(t, fake.calledRecover())
		})
	})
}
