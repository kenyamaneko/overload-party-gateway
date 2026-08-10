package accountclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func newStubAccountServer(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestPlayerResponse(playerID string) apiaccount.PlayerResponse {
	return apiaccount.PlayerResponse{
		PlayerID:         playerID,
		FirebaseUID:      "test-firebase-uid",
		Level:            1,
		LevelExpCurrent:  0,
		LevelExpRequired: 100,
		OnboardingStatus: apiaccount.OnboardingStatusCompleted,
		CreatedAt:        time.Unix(0, 0).UTC(),
		UpdatedAt:        time.Unix(0, 0).UTC(),
	}
}

func marshalPlayerResponse(t *testing.T, resp apiaccount.PlayerResponse) []byte {
	t.Helper()
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	return body
}

func TestAccountClient(t *testing.T) {
	t.Run("アカウント操作の下流エラー種別変換", func(t *testing.T) {
		tests := []struct {
			name   string
			status int
			body   []byte
			call   func(ctx context.Context, c *Client) error
			verify func(t *testing.T, err error)
		}{
			{
				name:   "accountサービスが正常応答を返したとき、エラーにならない",
				status: http.StatusOK,
				body:   marshalPlayerResponse(t, newTestPlayerResponse("TST-P1")),
				call: func(ctx context.Context, c *Client) error {
					_, err := c.GetMe(ctx)
					return err
				},
				verify: func(t *testing.T, err error) {
					require.NoError(t, err)
				},
			},
			{
				name:   "accountサービスが対象が見つからないことを表す応答(404)を返したとき、対象が見つからないことを表すエラーになる",
				status: http.StatusNotFound,
				body:   []byte(`{"error":"player not found"}`),
				call: func(ctx context.Context, c *Client) error {
					_, err := c.GetMe(ctx)
					return err
				},
				verify: func(t *testing.T, err error) {
					require.Error(t, err)
					assert.ErrorIs(t, err, port.ErrAccountNotFound)
				},
			},
			{
				name:   "accountサービスが重複を表す応答(409)を返したとき、既に登録済みであることを表すエラーになる",
				status: http.StatusConflict,
				body:   []byte(`{"error":"player already registered"}`),
				call: func(ctx context.Context, c *Client) error {
					_, err := c.Register(ctx, "test-firebase-uid")
					return err
				},
				verify: func(t *testing.T, err error) {
					require.Error(t, err)
					assert.ErrorIs(t, err, port.ErrPlayerAlreadyRegistered)
				},
			},
			{
				name:   "accountサービスが見つからない・重複のいずれにも当たらないエラー応答(401)を返したとき、元のエラーがそのまま伝播する",
				status: http.StatusUnauthorized,
				body:   []byte(`{"error":"account error"}`),
				call: func(ctx context.Context, c *Client) error {
					_, err := c.GetMe(ctx)
					return err
				},
				verify: func(t *testing.T, err error) {
					require.Error(t, err)
					assert.NotErrorIs(t, err, port.ErrAccountNotFound)
					assert.NotErrorIs(t, err, port.ErrPlayerAlreadyRegistered)
					assert.ErrorIs(t, err, apiaccountclient.ErrUnauthorized)
				},
			},
			{
				name:   "accountサービスが見つからない・重複のいずれにも当たらないエラー応答(403)を返したとき、元のエラーがそのまま伝播する",
				status: http.StatusForbidden,
				body:   []byte(`{"error":"account error"}`),
				call: func(ctx context.Context, c *Client) error {
					_, err := c.GetMe(ctx)
					return err
				},
				verify: func(t *testing.T, err error) {
					require.Error(t, err)
					assert.NotErrorIs(t, err, port.ErrAccountNotFound)
					assert.NotErrorIs(t, err, port.ErrPlayerAlreadyRegistered)
					assert.Contains(t, err.Error(), "403")
				},
			},
			{
				name:   "accountサービスが見つからない・重複のいずれにも当たらないエラー応答(500)を返したとき、元のエラーがそのまま伝播する",
				status: http.StatusInternalServerError,
				body:   []byte(`{"error":"account error"}`),
				call: func(ctx context.Context, c *Client) error {
					_, err := c.GetMe(ctx)
					return err
				},
				verify: func(t *testing.T, err error) {
					require.Error(t, err)
					assert.NotErrorIs(t, err, port.ErrAccountNotFound)
					assert.NotErrorIs(t, err, port.ErrPlayerAlreadyRegistered)
					assert.ErrorIs(t, err, apiaccountclient.ErrInternalServer)
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				srv := newStubAccountServer(t, tt.status, tt.body)
				c := New(srv.URL, srv.Client())

				err := tt.call(context.Background(), c)
				tt.verify(t, err)
			})
		}
	})
}

func TestFindByFirebaseUID(t *testing.T) {
	t.Run("Firebase UIDによるプレイヤー検索における未登録の扱い", func(t *testing.T) {
		t.Run("対象のFirebase UIDのプレイヤーが存在するとき、プレイヤー情報が返る", func(t *testing.T) {
			srv := newStubAccountServer(t, http.StatusOK, marshalPlayerResponse(t, newTestPlayerResponse("TST-P2")))
			c := New(srv.URL, srv.Client())

			player, err := c.FindByFirebaseUID(context.Background(), "test-firebase-uid")

			require.NoError(t, err)
			require.NotNil(t, player)
			assert.Equal(t, "TST-P2", player.PlayerID)
		})

		t.Run("対象のFirebase UIDのプレイヤーが存在しない(404)とき、エラーにはならずプレイヤー情報が存在しないことを表す結果になる", func(t *testing.T) {
			srv := newStubAccountServer(t, http.StatusNotFound, []byte(`{"error":"player not found"}`))
			c := New(srv.URL, srv.Client())

			player, err := c.FindByFirebaseUID(context.Background(), "test-firebase-uid")

			require.NoError(t, err)
			assert.Nil(t, player)
		})

		t.Run("accountサービスが未登録以外のエラー応答(500)を返したとき、元のエラーがそのまま伝播する", func(t *testing.T) {
			srv := newStubAccountServer(t, http.StatusInternalServerError, []byte(`{"error":"account error"}`))
			c := New(srv.URL, srv.Client())

			player, err := c.FindByFirebaseUID(context.Background(), "test-firebase-uid")

			require.Error(t, err)
			assert.Nil(t, player)
			assert.ErrorIs(t, err, apiaccountclient.ErrInternalServer)
		})
	})
}
