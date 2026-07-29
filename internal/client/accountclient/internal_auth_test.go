package accountclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newStatusServer は指定ステータスと body を返す account サービスのスタブを生成する。
func newStatusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestClient_InjectsInternalAuthHeader は呼び出しに内部認証トークンが X-Internal-Auth として乗ることを検証する。
func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	t.Run("X-Internal-Auth header の注入", func(t *testing.T) {
		t.Run("ctx に格納した token が X-Internal-Auth header として送られる", func(t *testing.T) {
			const wantToken = "test.jwt.token"
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := New(srv.URL)
			ctx := internalauth.WithToken(context.Background(), wantToken)
			require.NoError(t, c.IncrementBattleCount(ctx))
			assert.Equal(t, wantToken, got)
		})
	})
}

// TestClient_MapsDownstreamStatusToPortSentinel は account の HTTP ステータスが port sentinel に写像されることを検証する。
func TestClient_MapsDownstreamStatusToPortSentinel(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		call    func(context.Context, *Client) error
		wantErr error
	}{
		{
			name:    "重複登録の 409 は ErrPlayerAlreadyRegistered に写像する",
			status:  http.StatusConflict,
			call:    func(ctx context.Context, c *Client) error { _, err := c.Register(ctx, "uid-1"); return err },
			wantErr: port.ErrPlayerAlreadyRegistered,
		},
		{
			name:    "未登録プレイヤーの 404 は ErrAccountNotFound に写像する",
			status:  http.StatusNotFound,
			call:    func(ctx context.Context, c *Client) error { _, err := c.Login(ctx, "uid-1"); return err },
			wantErr: port.ErrAccountNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newStatusServer(t, tc.status, `{}`)
			c := New(srv.URL)

			err := tc.call(context.Background(), c)

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestClient_FindByFirebaseUID_FoldsNotFoundToNil は未登録 (404) を未登録の正常表現 (nil, nil) に畳むことを検証する。
func TestClient_FindByFirebaseUID_FoldsNotFoundToNil(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantPlayer bool
	}{
		{
			name:       "未登録は 404 を (nil, nil) に畳む",
			status:     http.StatusNotFound,
			wantPlayer: false,
		},
		{
			name:       "登録済みは player を返す",
			status:     http.StatusOK,
			wantPlayer: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newStatusServer(t, tc.status, `{}`)
			c := New(srv.URL)

			player, err := c.FindByFirebaseUID(context.Background(), "uid-1")

			require.NoError(t, err)
			assert.Equal(t, tc.wantPlayer, player != nil)
		})
	}
}
