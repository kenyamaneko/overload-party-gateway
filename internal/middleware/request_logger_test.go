package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedLogEntry struct {
	level slog.Level
	attrs map[string]slog.Value
}

type recordingLogHandler struct {
	mu      sync.Mutex
	entries []recordedLogEntry
}

func (h *recordingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	attrs := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value
		return true
	})
	h.entries = append(h.entries, recordedLogEntry{level: r.Level, attrs: attrs})
	return nil
}

func (h *recordingLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *recordingLogHandler) WithGroup(name string) slog.Handler       { return h }

func (h *recordingLogHandler) entryCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

func (h *recordingLogHandler) entry(i int) recordedLogEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.entries[i]
}

func withRecordingLogHandler(t *testing.T) *recordingLogHandler {
	t.Helper()
	prev := slog.Default()
	h := &recordingLogHandler{}
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func TestUseRequestLogger(t *testing.T) {
	t.Run("[リクエストログ]リクエストログの重大度決定", func(t *testing.T) {
		implicitCases := []struct {
			name      string
			status    int
			wantLevel slog.Level
		}{
			{
				name:      "明示的なログレベルの指定が無く、レスポンスのステータスコードが400未満(例: 399)のとき、Infoレベルで記録される",
				status:    399,
				wantLevel: slog.LevelInfo,
			},
			{
				name:      "明示的なログレベルの指定が無く、レスポンスのステータスコードが400以上(例: 400)のとき、Errorレベルで記録される",
				status:    400,
				wantLevel: slog.LevelError,
			},
		}

		for _, tc := range implicitCases {
			t.Run(tc.name, func(t *testing.T) {
				h := withRecordingLogHandler(t)
				r := gin.New()
				r.GET("/ping", UseRequestLogger(), func(c *gin.Context) {
					c.Status(tc.status)
				})

				req := httptest.NewRequest(http.MethodGet, "/ping", nil)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				require.Equal(t, 1, h.entryCount())
				assert.Equal(t, tc.wantLevel, h.entry(0).level)
			})
		}

		explicitCases := []struct {
			name          string
			explicitLevel slog.Level
			status        int
			wantLevel     slog.Level
		}{
			{
				name:          "明示的なログレベルとしてWarnを指定し、ステータスコードが200(400未満)のとき、Warnレベルで記録される",
				explicitLevel: slog.LevelWarn,
				status:        200,
				wantLevel:     slog.LevelWarn,
			},
			{
				name:          "明示的なログレベルとしてInfoを指定し、ステータスコードが500(400以上)のとき、Infoレベルで記録される",
				explicitLevel: slog.LevelInfo,
				status:        500,
				wantLevel:     slog.LevelInfo,
			},
		}

		for _, tc := range explicitCases {
			t.Run(tc.name, func(t *testing.T) {
				h := withRecordingLogHandler(t)
				r := gin.New()
				r.GET("/ping", UseRequestLogger(), func(c *gin.Context) {
					SetRequestLogLevel(c, tc.explicitLevel)
					c.Status(tc.status)
				})

				req := httptest.NewRequest(http.MethodGet, "/ping", nil)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				require.Equal(t, 1, h.entryCount())
				assert.Equal(t, tc.wantLevel, h.entry(0).level)
			})
		}
	})

	t.Run("[リクエストログ]アクセスログ出力", func(t *testing.T) {
		t.Run("いずれの場合も、リクエストの完了ごとに1回、実行したHTTPメソッド・パス・実際にクライアントへ返されたステータスコード・処理に要した時間を伴ってログが記録される", func(t *testing.T) {
			h := withRecordingLogHandler(t)
			r := gin.New()
			r.GET("/ping", UseRequestLogger(), func(c *gin.Context) {
				c.Status(http.StatusTeapot)
			})

			req := httptest.NewRequest(http.MethodGet, "/ping", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, 1, h.entryCount())
			entry := h.entry(0)
			require.Contains(t, entry.attrs, "method")
			require.Contains(t, entry.attrs, "path")
			require.Contains(t, entry.attrs, "status")
			require.Contains(t, entry.attrs, "latency")
			assert.Equal(t, http.MethodGet, entry.attrs["method"].String())
			assert.Equal(t, "/ping", entry.attrs["path"].String())
			assert.Equal(t, int64(http.StatusTeapot), entry.attrs["status"].Int64())
			assert.Greater(t, entry.attrs["latency"].Duration(), time.Duration(0))
		})
	})
}
