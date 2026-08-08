package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureRequestLog(t *testing.T, status int) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := gin.New()
	r.Use(UseRequestLogger())
	r.GET("/orders", func(c *gin.Context) {
		c.Status(status)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders", nil))

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	return record
}

func TestUseRequestLogger(t *testing.T) {
	t.Run("[HTTPミドルウェア]応答ステータスによるログレベルの分類", func(t *testing.T) {
		tests := []struct {
			name   string
			status int
			want   string
		}{
			{name: "応答が200のとき、ログレベルはINFOになる", status: http.StatusOK, want: "INFO"},
			{name: "応答が302のとき、ログレベルはINFOになる", status: http.StatusFound, want: "INFO"},
			{name: "応答が400のとき、ログレベルはERRORになる", status: http.StatusBadRequest, want: "ERROR"},
			{name: "応答が500のとき、ログレベルはERRORになる", status: http.StatusInternalServerError, want: "ERROR"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				record := captureRequestLog(t, tt.status)

				assert.Equal(t, tt.want, record["level"])
			})
		}
	})

	t.Run("[HTTPミドルウェア]記録される内容", func(t *testing.T) {
		t.Run("ログにリクエストのメソッド・パスと応答のステータスが記録される", func(t *testing.T) {
			record := captureRequestLog(t, http.StatusOK)

			assert.Equal(t, http.MethodGet, record["method"])
			assert.Equal(t, "/orders", record["path"])
			assert.Equal(t, float64(http.StatusOK), record["status"])
		})
	})
}
