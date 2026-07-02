package matchmakingclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingclient"
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

// TestClient_InjectsInternalAuthHeader は呼び出しに内部認証トークンが X-Internal-Auth として乗ることを検証する。
func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	const wantToken = "test.jwt.token"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(internalauth.HeaderName)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx := internalauth.WithToken(context.Background(), wantToken)
	if err := c.Enqueue(ctx, 42, "alice", 7); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got != wantToken {
		t.Errorf("X-Internal-Auth = %q, want %q", got, wantToken)
	}
}

// TestClient_Enqueue_MapsStatusToSentinel は受付停止 (503) を port sentinel に写像し、その他の失敗は SDK sentinel を透過することを検証する。
func TestClient_Enqueue_MapsStatusToSentinel(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{
			name:    "受付停止の 503 は ErrMatchmakingUnavailable に写像する",
			status:  http.StatusServiceUnavailable,
			wantErr: port.ErrMatchmakingUnavailable,
		},
		{
			name:    "その他の 5xx は SDK sentinel を透過する",
			status:  http.StatusInternalServerError,
			wantErr: apimatchmakingclient.ErrInternalServer,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newStatusServer(t, tc.status)
			c := New(srv.URL)

			err := c.Enqueue(context.Background(), 42, "alice", 7)

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestClient_Cancel_FoldsAndMapsStatus はキャンセルが 404/200 を成功に畳み、受付停止を port sentinel に写像することを検証する。
func TestClient_Cancel_FoldsAndMapsStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{
			name:    "取消成功の 200 は nil を返す",
			status:  http.StatusOK,
			wantErr: nil,
		},
		{
			name:    "キュー未登録の 404 は nil に畳む",
			status:  http.StatusNotFound,
			wantErr: nil,
		},
		{
			name:    "受付停止の 503 は ErrMatchmakingUnavailable に写像する",
			status:  http.StatusServiceUnavailable,
			wantErr: port.ErrMatchmakingUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newStatusServer(t, tc.status)
			c := New(srv.URL)

			err := c.Cancel(context.Background())

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
