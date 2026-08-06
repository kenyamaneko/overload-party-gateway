package accountclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
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

func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	t.Run("X-Internal-Auth headerの注入", func(t *testing.T) {
		t.Run("ctxに格納したtokenがX-Internal-Auth headerとして送られる", func(t *testing.T) {
			const wantToken = "test.jwt.token"
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := New(srv.URL, &http.Client{})
			ctx := internalauth.WithToken(context.Background(), wantToken)
			require.NoError(t, c.IncrementBattleCount(ctx))
			assert.Equal(t, wantToken, got)
		})
	})
}

func TestClient_MapsDownstreamStatusToPortSentinel(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		call    func(context.Context, *Client) error
		wantErr error
	}{
		{
			name:    "重複登録の409はErrPlayerAlreadyRegisteredに写像する",
			status:  http.StatusConflict,
			call:    func(ctx context.Context, c *Client) error { _, err := c.Register(ctx, "uid-1"); return err },
			wantErr: port.ErrPlayerAlreadyRegistered,
		},
		{
			name:    "未登録プレイヤーの404はErrAccountNotFoundに写像する",
			status:  http.StatusNotFound,
			call:    func(ctx context.Context, c *Client) error { _, err := c.Login(ctx, "uid-1"); return err },
			wantErr: port.ErrAccountNotFound,
		},
	}

	t.Run("登録・ログインの失敗応答の変換", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newStatusServer(t, tc.status, `{}`)
				c := New(srv.URL, &http.Client{})

				err := tc.call(context.Background(), c)

				require.ErrorIs(t, err, tc.wantErr)
			})
		}
	})
}

func TestClient_FindByFirebaseUID(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantPlayer *apiaccount.PlayerResponse
	}{
		{
			name:       "未登録は404を (nil, nil)に畳む",
			status:     http.StatusNotFound,
			body:       "",
			wantPlayer: nil,
		},
		{
			name:   "登録済みはplayer本体を返す",
			status: http.StatusOK,
			body:   `{"player_id":"player-1","firebase_uid":"uid-1"}`,
			wantPlayer: &apiaccount.PlayerResponse{
				PlayerID:    "player-1",
				FirebaseUID: "uid-1",
			},
		},
	}

	t.Run("Firebase UIDによるプレイヤー検索", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newStatusServer(t, tc.status, tc.body)
				c := New(srv.URL, &http.Client{})

				player, err := c.FindByFirebaseUID(context.Background(), "uid-1")

				require.NoError(t, err)
				assert.Equal(t, tc.wantPlayer, player)
			})
		}
	})
}
