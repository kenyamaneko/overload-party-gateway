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

// UseRequestLogger は HTTP リクエストの結果を記録する Gin middleware を返す。
// レベルは SetRequestLogLevel の指定を優先し、未指定なら 2xx/3xx=Info・4xx/5xx=Error とする。
func UseRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusBadRequest {
			level = slog.LevelError
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
