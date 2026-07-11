package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// withFirebaseUID は firebase_uid を context に注入する middleware ヘルパー。
// UseFirebaseAuth が成功した状態を再現する。
func withFirebaseUID(uid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uid != "" {
			c.Set("firebase_uid", uid)
		}
		c.Next()
	}
}

func TestAuthHandler_Register(t *testing.T) {
	t.Run("プレイヤー登録", func(t *testing.T) {
		t.Run("登録成功のとき、201 と name 未設定のプレイヤーを返す", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.RegisterFn = func(req apiaccount.RegisterRequest) (int, any) {
				return http.StatusCreated, apiaccount.PlayerResponse{
					PlayerID:    "p-" + req.FirebaseUID,
					FirebaseUID: req.FirebaseUID,
					// Name はオンボーディング完了まで nil。account 側も nil で挿入する契約。
					Name: nil,
				}
			}
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid-new"))
			r.POST("/register", h.Register)

			req := httptest.NewRequest(http.MethodPost, "/register", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"player_id":"p-uid-new"`)
			// name は omitempty なので JSON に出ない。
			assert.NotContains(t, w.Body.String(), `"name":`)
		})

		t.Run("firebase_uid が無いとき、401 になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.POST("/register", h.Register)

			req := httptest.NewRequest(http.MethodPost, "/register", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("account が 409 を返すとき、409 になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.RegisterFn = func(_ apiaccount.RegisterRequest) (int, any) {
				return http.StatusConflict, nil
			}
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid-dup"))
			r.POST("/register", h.Register)

			req := httptest.NewRequest(http.MethodPost, "/register", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusConflict, w.Code)
		})

		t.Run("account が 500 を返すとき、500 になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.RegisterFn = func(_ apiaccount.RegisterRequest) (int, any) {
				return http.StatusInternalServerError, nil
			}
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid-x"))
			r.POST("/register", h.Register)

			req := httptest.NewRequest(http.MethodPost, "/register", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	})
}

func TestAuthHandler_Login(t *testing.T) {
	t.Run("プレイヤーログイン", func(t *testing.T) {
		t.Run("ログイン成功のとき、200 とプレイヤーを返す", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.LoginFn = func(req apiaccount.LoginRequest) (int, any) {
				name := "x"
				return http.StatusOK, apiaccount.PlayerResponse{
					PlayerID:    "p-x",
					FirebaseUID: req.FirebaseUID,
					Name:        &name,
				}
			}
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid-x"))
			r.POST("/login", h.Login)

			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"player_id":"p-x"`)
		})

		t.Run("未登録のとき、404 になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.LoginFn = func(_ apiaccount.LoginRequest) (int, any) {
				return http.StatusNotFound, nil
			}
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid-unknown"))
			r.POST("/login", h.Login)

			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run("firebase_uid が無いとき、401 になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.POST("/login", h.Login)

			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("account が 500 を返すとき、500 になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.LoginFn = func(_ apiaccount.LoginRequest) (int, any) {
				return http.StatusInternalServerError, nil
			}
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid"))
			r.POST("/login", h.Login)

			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	})
}
