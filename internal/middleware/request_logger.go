package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const requestLogLevelKey = "request_log_level"

// SetRequestLogLevel は UseRequestLogger が記録するログレベルを上書きする。
func SetRequestLogLevel(c *gin.Context, level slog.Level) {
	c.Set(requestLogLevelKey, level)
}

// UseRequestLogger は HTTP リクエストの結果をステータスコードに応じたレベルで
// 記録する Gin middleware を返す (5xx=Error / 4xx=Warn / それ以外=Info)。
func UseRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		} else if status >= http.StatusBadRequest {
			// クライアント起因のエラー (4xx) は運用に支障をきたさないため、Warn 止まりとする。
			level = slog.LevelWarn
		}
		if explicit, ok := c.Get(requestLogLevelKey); ok {
			level = explicit.(slog.Level)
		}

		slog.LogAttrs(c.Request.Context(), level, "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
		)
	}
}
