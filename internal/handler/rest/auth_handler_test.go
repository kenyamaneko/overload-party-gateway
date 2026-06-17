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
	t.Run("success returns 201 with name unset", func(t *testing.T) {
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

	t.Run("missing firebase uid returns 401", func(t *testing.T) {
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

	t.Run("conflict from account service returns 409", func(t *testing.T) {
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

	t.Run("downstream 500 returns 500", func(t *testing.T) {
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
}

func TestAuthHandler_Login(t *testing.T) {
	t.Run("success", func(t *testing.T) {
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

	t.Run("not registered returns 404", func(t *testing.T) {
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

	t.Run("missing firebase uid returns 401", func(t *testing.T) {
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

	t.Run("downstream 500", func(t *testing.T) {
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
}

