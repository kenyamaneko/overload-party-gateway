package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
)

func getVersionBody(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()

	r := gin.New()
	r.GET("/version", NewStaticHandler(cfg).GetVersion)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/version", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestStaticHandler_GetVersion(t *testing.T) {
	t.Run("[設定]アプリバージョン情報の取得", func(t *testing.T) {
		t.Run("最低バージョン・最新バージョン・強制アップデート要否・ストアURLが設定されているとき、その4つが返る", func(t *testing.T) {
			body := getVersionBody(t, &config.Config{
				AppMinVersion:    "1.0.0",
				AppLatestVersion: "1.2.0",
				AppForceUpdate:   true,
				AppStoreURL:      "https://store.example.com/app",
			})

			assert.Equal(t, "1.0.0", body["minimumVersion"])
			assert.Equal(t, "1.2.0", body["latestVersion"])
			assert.Equal(t, true, body["forceUpdate"])
			assert.Equal(t, "https://store.example.com/app", body["storeUrl"])
		})

		t.Run("ストアURLが未設定のとき、ストアURLは空文字として返り、項目自体は省略されない", func(t *testing.T) {
			body := getVersionBody(t, &config.Config{
				AppMinVersion:    "0.1.0",
				AppLatestVersion: "0.1.0",
				AppForceUpdate:   false,
				AppStoreURL:      "",
			})

			require.Contains(t, body, "storeUrl")
			assert.Equal(t, "", body["storeUrl"])
		})
	})
}
