package rest

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func newUnauthenticatedRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest(method, path, nil)
}

func newDevAuthenticatedRequest(t *testing.T, method, path, firebaseUID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+middleware.DevTokenPrefix+firebaseUID)
	return req
}

func newAuthEngineWithoutAuth(account port.AccountClient) *gin.Engine {
	h := NewAuthHandler(account)
	r := gin.New()
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	return r
}

func newAuthEngineWithDevAuth(account port.AccountClient) *gin.Engine {
	h := NewAuthHandler(account)
	r := gin.New()
	r.Use(middleware.UseDevAuth())
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	return r
}

func decodePlayerID(t *testing.T, body []byte) string {
	t.Helper()
	var got struct {
		PlayerID string `json:"player_id"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	return got.PlayerID
}

func decodeErrorField(t *testing.T, body []byte) string {
	t.Helper()
	var got struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	return got.Error
}

func newAuthEngineWithRequestLogger(account port.AccountClient) *gin.Engine {
	h := NewAuthHandler(account)
	r := gin.New()
	r.Use(middleware.UseDevAuth())
	r.Use(middleware.UseRequestLogger())
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	return r
}

type capturingSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
	return nil
}

func (h *capturingSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingSlogHandler) WithGroup(_ string) slog.Handler      { return h }

func captureRequestLogLevel(t *testing.T, doRequest func()) slog.Level {
	t.Helper()
	prevLogger := slog.Default()
	// slog.SetDefault は復帰先が slog 標準の handler だと判定すると log.SetOutput を戻さないため、
	// log パッケージ側の出力先も明示的に保存して復元する。
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	handler := &capturingSlogHandler{}
	slog.SetDefault(slog.New(handler))
	defer func() {
		slog.SetDefault(prevLogger)
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	}()

	doRequest()

	require.Len(t, handler.records, 1, "UseRequestLogger はリクエストごとに1件のログを記録する")
	return handler.records[0].Level
}

func TestAuthHandlerRegister(t *testing.T) {
	t.Run("[認証]新規プレイヤー登録の受付", func(t *testing.T) {
		t.Run("認証情報にユーザー識別子が含まれていないとき、ステータスコード401で、認証情報が無い旨のエラー内容を返す", func(t *testing.T) {
			r := newAuthEngineWithoutAuth(&stubAccountClient{})
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newUnauthenticatedRequest(t, http.MethodPost, "/register"))

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing firebase uid")
		})

		t.Run("ユーザー識別子はあるが、そのユーザーがまだ登録されていない状態で登録を要求したとき、登録が成立し、ステータスコード201で新しく作成されたプレイヤー情報を返す", func(t *testing.T) {
			created := &apiaccount.PlayerResponse{
				PlayerID:         "player-new-001",
				FirebaseUID:      "uid-not-yet-registered",
				OnboardingStatus: apiaccount.OnboardingStatusCompleted,
			}
			account := &stubAccountClient{
				registerFunc: func(_ context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
					assert.Equal(t, "uid-not-yet-registered", firebaseUID)
					return created, nil
				},
			}
			r := newAuthEngineWithDevAuth(account)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/register", "uid-not-yet-registered"))

			require.Equal(t, http.StatusCreated, w.Code)
			assert.Equal(t, created.PlayerID, decodePlayerID(t, w.Body.Bytes()))
		})

		t.Run("ユーザー識別子が既に登録済みの状態で登録を要求したとき、登録は成立せず、ステータスコード409で登録済みである旨のエラー内容を返す", func(t *testing.T) {
			account := &stubAccountClient{
				registerFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
					return nil, port.ErrPlayerAlreadyRegistered
				},
			}
			r := newAuthEngineWithDevAuth(account)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/register", "uid-already-registered"))

			assert.Equal(t, http.StatusConflict, w.Code)
			assert.Contains(t, w.Body.String(), "port: player already registered")
		})

		t.Run("登録要求の処理自体が、未登録での新規登録の成立でも、既に登録済みであることによる拒否でもない理由で失敗したとき、ステータスコード500でエラー内容を返す", func(t *testing.T) {
			account := &stubAccountClient{
				registerFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
					return nil, errors.New("account service unavailable")
				},
			}
			r := newAuthEngineWithDevAuth(account)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/register", "uid-arbitrary-failure"))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.NotEmpty(t, decodeErrorField(t, w.Body.Bytes()))
		})
	})

	t.Run("[リクエストログ]異常系ログの重大度の切り分け", func(t *testing.T) {
		t.Run("新規プレイヤー登録の受付で、ユーザー識別子が既に登録済みだったとき(409)、Warnレベルでログに記録される", func(t *testing.T) {
			account := &stubAccountClient{
				registerFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
					return nil, port.ErrPlayerAlreadyRegistered
				},
			}
			r := newAuthEngineWithRequestLogger(account)

			level := captureRequestLogLevel(t, func() {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/register", "uid-409-log-severity"))
				require.Equal(t, http.StatusConflict, w.Code)
			})

			assert.Equal(t, slog.LevelWarn, level)
		})
	})
}

func TestAuthHandlerLogin(t *testing.T) {
	t.Run("[認証]プレイヤーログインの受付", func(t *testing.T) {
		t.Run("認証情報にユーザー識別子が含まれていないとき、ステータスコード401で、認証情報が無い旨のエラー内容を返す", func(t *testing.T) {
			r := newAuthEngineWithoutAuth(&stubAccountClient{})
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newUnauthenticatedRequest(t, http.MethodPost, "/login"))

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing firebase uid")
		})

		t.Run("ユーザー識別子に対応するプレイヤーが登録済みのとき、ステータスコード200で該当プレイヤー情報を返す", func(t *testing.T) {
			existing := &apiaccount.PlayerResponse{
				PlayerID:         "player-existing-001",
				FirebaseUID:      "uid-registered",
				OnboardingStatus: apiaccount.OnboardingStatusCompleted,
			}
			account := &stubAccountClient{
				loginFunc: func(_ context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
					assert.Equal(t, "uid-registered", firebaseUID)
					return existing, nil
				},
			}
			r := newAuthEngineWithDevAuth(account)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/login", "uid-registered"))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, existing.PlayerID, decodePlayerID(t, w.Body.Bytes()))
		})

		t.Run("ユーザー識別子に対応するプレイヤーが登録されていないとき、ステータスコード404で未登録である旨のエラー内容を返す", func(t *testing.T) {
			account := &stubAccountClient{
				loginFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
					return nil, port.ErrAccountNotFound
				},
			}
			r := newAuthEngineWithDevAuth(account)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/login", "uid-unregistered"))

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Contains(t, w.Body.String(), "port: account player not found")
		})

		t.Run("ログイン処理自体が、プレイヤーが登録済みであることによる成功でも、未登録であることによる404でもない理由で失敗したとき、ステータスコード500でエラー内容を返す", func(t *testing.T) {
			account := &stubAccountClient{
				loginFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
					return nil, errors.New("account service unavailable")
				},
			}
			r := newAuthEngineWithDevAuth(account)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/login", "uid-arbitrary-failure"))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.NotEmpty(t, decodeErrorField(t, w.Body.Bytes()))
		})
	})

	t.Run("[リクエストログ]異常系ログの重大度の切り分け", func(t *testing.T) {
		t.Run("プレイヤーログインの受付で、ユーザー識別子に対応するプレイヤーが登録されていなかったとき(404)、Infoレベルでログに記録される", func(t *testing.T) {
			account := &stubAccountClient{
				loginFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
					return nil, port.ErrAccountNotFound
				},
			}
			r := newAuthEngineWithRequestLogger(account)

			level := captureRequestLogLevel(t, func() {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, newDevAuthenticatedRequest(t, http.MethodPost, "/login", "uid-404-log-severity"))
				require.Equal(t, http.StatusNotFound, w.Code)
			})

			assert.Equal(t, slog.LevelInfo, level)
		})
	})
}
