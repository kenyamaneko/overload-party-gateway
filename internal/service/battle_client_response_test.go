package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// newBattleServer は固定のステータスと body を返す battle server スタブを生成する。
func newBattleServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBattleClient_ProcessAction(t *testing.T) {
	t.Run("アクションデータの変換と送信", func(t *testing.T) {
		t.Run("不正な JSON データのとき、battle に送信せずエラーになる", func(t *testing.T) {
			wasPosted := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				wasPosted = true
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			c := NewBattleClient(srv.URL)

			_, err := c.ProcessAction(context.Background(), "game-1", 1, "play_card", json.RawMessage(`{`))

			require.Error(t, err)
			require.False(t, wasPosted)
		})

		sendCases := []struct {
			name     string
			data     json.RawMessage
			wantData interface{}
		}{
			{
				name:     "data が nil のとき、data フィールドなしで送られる",
				data:     nil,
				wantData: nil,
			},
			{
				name:     "オブジェクトのとき、map として送られる",
				data:     json.RawMessage(`{"target":"TST-0001"}`),
				wantData: map[string]interface{}{"target": "TST-0001"},
			},
		}

		for _, tc := range sendCases {
			t.Run(tc.name, func(t *testing.T) {
				var sent map[string]interface{}
				var decodeErr error
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					decodeErr = json.NewDecoder(r.Body).Decode(&sent)
					_, _ = w.Write([]byte(`{}`))
				}))
				t.Cleanup(srv.Close)
				c := NewBattleClient(srv.URL)

				_, err := c.ProcessAction(context.Background(), "game-1", 1, "play_card", tc.data)

				require.NoError(t, err)
				require.NoError(t, decodeErr)
				require.Equal(t, tc.wantData, sent["data"])
			})
		}
	})
}

func TestBattleClient_GetGameStateForPlayer(t *testing.T) {
	t.Run("状態取得のステータス処理", func(t *testing.T) {
		statusCases := []struct {
			name    string
			status  int
			body    string
			wantRaw json.RawMessage
			wantErr error
		}{
			{
				name:    "404 のとき、状態欠落として errMissingGameState を返す",
				status:  http.StatusNotFound,
				body:    `{"error":"game not found"}`,
				wantErr: errMissingGameState,
			},
			{
				name:    "200 のとき、body を raw として返す",
				status:  http.StatusOK,
				body:    `{"phase":"main"}`,
				wantRaw: json.RawMessage(`{"phase":"main"}`),
			},
		}

		for _, tc := range statusCases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL)

				got, err := c.GetGameStateForPlayer(context.Background(), "game-1", 1)
				require.ErrorIs(t, err, tc.wantErr)
				require.Equal(t, tc.wantRaw, got)
			})
		}

		errorCases := []struct {
			name    string
			status  int
			body    string
			wantMsg string
		}{
			{
				name:    "400 の構造化エラーのとき、message をエラー文に含める",
				status:  http.StatusBadRequest,
				body:    `{"error":"invalid player"}`,
				wantMsg: "invalid player",
			},
			{
				name:    "500 の非 JSON エラーのとき、ステータスコードと body 文字列を含める",
				status:  http.StatusInternalServerError,
				body:    "boom",
				wantMsg: "battle server returned 500: boom",
			},
		}

		for _, tc := range errorCases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL)

				got, err := c.GetGameStateForPlayer(context.Background(), "game-1", 1)
				require.Nil(t, got)
				require.EqualError(t, err, tc.wantMsg)
			})
		}
	})
}

func TestBattleClient_GetTurnControlsForPlayer(t *testing.T) {
	t.Run("turn controls の不在判定", func(t *testing.T) {
		cases := []struct {
			name    string
			status  int
			body    string
			wantRaw json.RawMessage
		}{
			{
				name:    "null body のとき、不在として nil を返す",
				status:  http.StatusOK,
				body:    "null",
				wantRaw: nil,
			},
			{
				name:    "空 body のとき、不在として nil を返す",
				status:  http.StatusOK,
				body:    "",
				wantRaw: nil,
			},
			{
				name:    "404 のとき、不在として nil を返す",
				status:  http.StatusNotFound,
				body:    "",
				wantRaw: nil,
			},
			{
				name:    "controls が存在するとき、raw を返す",
				status:  http.StatusOK,
				body:    `{"controls":["end_turn"]}`,
				wantRaw: json.RawMessage(`{"controls":["end_turn"]}`),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL)

				got, err := c.GetTurnControlsForPlayer(context.Background(), "game-1", 1)
				require.NoError(t, err)
				require.Equal(t, tc.wantRaw, got)
			})
		}
	})
}

func TestBattleClient_PostErrorResponse(t *testing.T) {
	t.Run("送信系の非 200 応答のエラー変換", func(t *testing.T) {
		cases := []struct {
			name    string
			status  int
			body    string
			wantMsg string
			call    func(context.Context, BattleClient) error
		}{
			{
				name:    "アクション送信が 400 の構造化エラーを受けたとき、message をエラー文に含める",
				status:  http.StatusBadRequest,
				body:    `{"error":"invalid action"}`,
				wantMsg: "invalid action",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.ProcessAction(ctx, "game-1", 1, "play_card", json.RawMessage(`{}`))
					return err
				},
			},
			{
				name:    "NPC 対戦の作成が 500 の非 JSON エラーを受けたとき、ステータスコードと body 文字列を含める",
				status:  http.StatusInternalServerError,
				body:    "boom",
				wantMsg: "battle server returned 500: boom",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.StartNPCBattle(ctx, nil, DeckInitiatives{}, "npc-1", PlayerSummaryRequest{}, PlayerSummaryRequest{})
					return err
				},
			},
			{
				name:    "PvP 対戦の作成が 400 の構造化エラーを受けたとき、message をエラー文に含める",
				status:  http.StatusBadRequest,
				body:    `{"error":"deck mismatch"}`,
				wantMsg: "deck mismatch",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.CreatePvPGame(ctx, nil, nil, DeckInitiatives{}, DeckInitiatives{}, PlayerSummaryRequest{}, PlayerSummaryRequest{})
					return err
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL)

				err := tc.call(context.Background(), c)

				require.EqualError(t, err, tc.wantMsg)
			})
		}
	})
}
