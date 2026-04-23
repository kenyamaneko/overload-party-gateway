package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
// FirebaseAuth が成功した状態を再現する。
func withFirebaseUID(uid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uid != "" {
			c.Set("firebase_uid", uid)
		}
		c.Next()
	}
}

func TestAuthHandler_Register(t *testing.T) {
	t.Run("success returns 201 and player", func(t *testing.T) {
		fa := apiaccountserverfake.NewServer()
		defer fa.Close()
		fa.RegisterFn = func(req apiaccount.RegisterRequest) (int, any) {
			return http.StatusCreated, apiaccount.Player{
				PlayerID:    "p-" + req.FirebaseUID,
				FirebaseUID: req.FirebaseUID,
				Username:    req.Username,
			}
		}
		h := NewAuthHandler(accountclient.New(fa.URL()))

		r := gin.New()
		r.Use(withFirebaseUID("uid-new"))
		r.POST("/register", h.Register)

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"username":"alice"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"player_id":"p-uid-new"`)
		assert.Contains(t, w.Body.String(), `"username":"alice"`)
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		fa := apiaccountserverfake.NewServer()
		defer fa.Close()
		h := NewAuthHandler(accountclient.New(fa.URL()))

		r := gin.New()
		r.Use(withFirebaseUID("uid"))
		r.POST("/register", h.Register)

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{not json`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing firebase uid returns 401", func(t *testing.T) {
		fa := apiaccountserverfake.NewServer()
		defer fa.Close()
		h := NewAuthHandler(accountclient.New(fa.URL()))

		r := gin.New()
		r.POST("/register", h.Register)

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"username":"alice"}`))
		req.Header.Set("Content-Type", "application/json")
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

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"username":"bob"}`))
		req.Header.Set("Content-Type", "application/json")
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

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"username":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestAuthHandler_Register_UsernameValidation(t *testing.T) {
	tests := []struct {
		name     string
		username string
	}{
		{"empty", ""},
		{"whitespace_only", "   "},
		{"contains_newline", "ab\ncd"},
		{"contains_tab", "ab\tcd"},
		{"too_long", strings.Repeat("a", 51)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			h := NewAuthHandler(accountclient.New(fa.URL()))

			r := gin.New()
			r.Use(withFirebaseUID("uid"))
			r.POST("/register", h.Register)

			body, _ := json.Marshal(map[string]string{"username": tt.username})
			req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func TestAuthHandler_Register_BoundaryUsername(t *testing.T) {
	t.Run("max length 50 multibyte runes ok", func(t *testing.T) {
		fa := apiaccountserverfake.NewServer()
		defer fa.Close()
		// 既定の RegisterFn (nil) は空 Player を返すので、handler が 201 を
		// そのまま返せば多バイト文字長チェックを通過した合図になる。
		h := NewAuthHandler(accountclient.New(fa.URL()))

		r := gin.New()
		r.Use(withFirebaseUID("uid-50"))
		r.POST("/register", h.Register)

		// 50 ja runes (multibyte) — RuneCount でカウントされること
		username := strings.Repeat("あ", 50)
		body, _ := json.Marshal(map[string]string{"username": username})
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	})
}

func TestAuthHandler_Login(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fa := apiaccountserverfake.NewServer()
		defer fa.Close()
		fa.LoginFn = func(req apiaccount.LoginRequest) (int, any) {
			return http.StatusOK, apiaccount.Player{
				PlayerID:    "p-x",
				FirebaseUID: req.FirebaseUID,
				Username:    "x",
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

func TestValidateUsername(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		assert.NoError(t, validateUsername("alice"))
		assert.NoError(t, validateUsername("あいう"))
		assert.NoError(t, validateUsername("a b c"))
	})
	t.Run("ng", func(t *testing.T) {
		assert.Error(t, validateUsername(""))
		assert.Error(t, validateUsername("   "))
		assert.Error(t, validateUsername("a\nb"))
		assert.Error(t, validateUsername(strings.Repeat("a", 51)))
	})
}
