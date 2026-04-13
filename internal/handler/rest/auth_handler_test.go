package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// fakeAccountServer は accountclient が期待するエンドポイントだけを実装した httptest.Server。
// handler は具体型 *accountclient.Client を受け取るため、interface 化せず本物のクライアントを fake URL に向ける。
type fakeAccountServer struct {
	mu       sync.Mutex
	server   *httptest.Server
	players  map[string]*accountclient.Player // firebaseUID → player
	register func(uid, username string) (int, *accountclient.Player)
	login    func(uid string) (int, *accountclient.Player)
}

func newFakeAccountServer() *fakeAccountServer {
	fa := &fakeAccountServer{players: map[string]*accountclient.Player{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/internal/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			FirebaseUID string `json:"firebase_uid"`
			Username    string `json:"username"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fa.mu.Lock()
		defer fa.mu.Unlock()
		if fa.register != nil {
			code, p := fa.register(body.FirebaseUID, body.Username)
			w.WriteHeader(code)
			if p != nil {
				_ = json.NewEncoder(w).Encode(p)
			}
			return
		}
		if _, exists := fa.players[body.FirebaseUID]; exists {
			w.WriteHeader(http.StatusConflict)
			return
		}
		p := &accountclient.Player{
			PlayerID:    "p-" + body.FirebaseUID,
			FirebaseUID: body.FirebaseUID,
			Username:    body.Username,
		}
		fa.players[body.FirebaseUID] = p
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(p)
	})

	mux.HandleFunc("/internal/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			FirebaseUID string `json:"firebase_uid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fa.mu.Lock()
		defer fa.mu.Unlock()
		if fa.login != nil {
			code, p := fa.login(body.FirebaseUID)
			w.WriteHeader(code)
			if p != nil {
				_ = json.NewEncoder(w).Encode(p)
			}
			return
		}
		p, ok := fa.players[body.FirebaseUID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	})

	fa.server = httptest.NewServer(mux)
	return fa
}

func (f *fakeAccountServer) close()                           { f.server.Close() }
func (f *fakeAccountServer) client() *accountclient.Client    { return accountclient.New(f.server.URL) }
func (f *fakeAccountServer) seed(uid string, p *accountclient.Player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.players[uid] = p
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
		fa := newFakeAccountServer()
		defer fa.close()
		h := NewAuthHandler(fa.client())

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
		fa := newFakeAccountServer()
		defer fa.close()
		h := NewAuthHandler(fa.client())

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
		fa := newFakeAccountServer()
		defer fa.close()
		h := NewAuthHandler(fa.client())

		r := gin.New()
		// no withFirebaseUID
		r.POST("/register", h.Register)

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"username":"alice"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("conflict from account service returns 409", func(t *testing.T) {
		fa := newFakeAccountServer()
		defer fa.close()
		// pre-seed → register handler returns 409
		fa.seed("uid-dup", &accountclient.Player{PlayerID: "x", FirebaseUID: "uid-dup", Username: "bob"})
		h := NewAuthHandler(fa.client())

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
		fa := newFakeAccountServer()
		defer fa.close()
		fa.register = func(uid, username string) (int, *accountclient.Player) {
			return http.StatusInternalServerError, nil
		}
		h := NewAuthHandler(fa.client())

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
			fa := newFakeAccountServer()
			defer fa.close()
			h := NewAuthHandler(fa.client())

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
		fa := newFakeAccountServer()
		defer fa.close()
		h := NewAuthHandler(fa.client())

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
		fa := newFakeAccountServer()
		defer fa.close()
		fa.seed("uid-x", &accountclient.Player{PlayerID: "p-x", FirebaseUID: "uid-x", Username: "x"})
		h := NewAuthHandler(fa.client())

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
		fa := newFakeAccountServer()
		defer fa.close()
		h := NewAuthHandler(fa.client())

		r := gin.New()
		r.Use(withFirebaseUID("uid-unknown"))
		r.POST("/login", h.Login)

		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("missing firebase uid returns 401", func(t *testing.T) {
		fa := newFakeAccountServer()
		defer fa.close()
		h := NewAuthHandler(fa.client())

		r := gin.New()
		r.POST("/login", h.Login)

		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("downstream 500", func(t *testing.T) {
		fa := newFakeAccountServer()
		defer fa.close()
		fa.login = func(uid string) (int, *accountclient.Player) { return http.StatusInternalServerError, nil }
		h := NewAuthHandler(fa.client())

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
