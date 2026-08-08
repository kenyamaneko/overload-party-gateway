package matchmakingclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newStatusServer は指定ステータスを返す matchmaking サービスのスタブを生成する。
func newStatusServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	t.Run("[内部認証]matchmaking宛リクエストへのX-Internal-Authヘッダーの注入", func(t *testing.T) {
		t.Run("トークンを格納したコンテキストのとき、X-Internal-Authヘッダーとして送信される", func(t *testing.T) {
			const wantToken = "test.jwt.token"
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.WriteHeader(http.StatusAccepted)
			}))
			defer srv.Close()

			c := New(srv.URL, "test-instance-id", &http.Client{})
			ctx := internalauth.WithToken(context.Background(), wantToken)
			require.NoError(t, c.Enqueue(ctx, 42, "alice", 7))
			assert.Equal(t, wantToken, got)
		})
	})
}

func TestClient_Enqueue_MapsStatusToSentinel(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{
			name:    "受付停止の503は呼び出し元がリトライ可能と判別できるエラーになる",
			status:  http.StatusServiceUnavailable,
			wantErr: port.ErrMatchmakingUnavailable,
		},
		{
			name:    "503以外の5xxは受付停止として扱われないエラーが返る",
			status:  http.StatusInternalServerError,
			wantErr: apimatchmakingclient.ErrInternalServer,
		},
	}

	t.Run("[マッチング]マッチング登録の失敗応答の変換", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newStatusServer(t, tc.status)
				c := New(srv.URL, "test-instance-id", &http.Client{})

				err := c.Enqueue(context.Background(), 42, "alice", 7)

				require.ErrorIs(t, err, tc.wantErr)
			})
		}
	})
}

func TestClient_Cancel_FoldsAndMapsStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{
			name:    "取消成功の200はnilを返す",
			status:  http.StatusOK,
			wantErr: nil,
		},
		{
			name:    "キュー未登録の404はnilに畳む",
			status:  http.StatusNotFound,
			wantErr: nil,
		},
		{
			name:    "受付停止の503は呼び出し元がリトライ可能と判別できるエラーになる",
			status:  http.StatusServiceUnavailable,
			wantErr: port.ErrMatchmakingUnavailable,
		},
	}

	t.Run("[マッチング]マッチング取消の応答の変換", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newStatusServer(t, tc.status)
				c := New(srv.URL, "test-instance-id", &http.Client{})

				err := c.Cancel(context.Background())

				require.ErrorIs(t, err, tc.wantErr)
			})
		}
	})
}
