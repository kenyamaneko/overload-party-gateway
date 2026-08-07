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
	t.Run("[対戦連携]アクションデータの変換と送信", func(t *testing.T) {
		t.Run("不正なJSONデータのとき、battleに送信せずエラーになる", func(t *testing.T) {
			wasPosted := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				wasPosted = true
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			c := NewBattleClient(srv.URL, &http.Client{})

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
				name:     "dataがnilのとき、dataフィールドなしで送られる",
				data:     nil,
				wantData: nil,
			},
			{
				name:     "オブジェクトのとき、mapとして送られる",
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
				c := NewBattleClient(srv.URL, &http.Client{})

				_, err := c.ProcessAction(context.Background(), "game-1", 1, "play_card", tc.data)

				require.NoError(t, err)
				require.NoError(t, decodeErr)
				require.Equal(t, tc.wantData, sent["data"])
			})
		}
	})
}

func TestBattleClient_GetGameStateForPlayer(t *testing.T) {
	t.Run("[対戦連携]状態取得のステータス処理", func(t *testing.T) {
		statusCases := []struct {
			name    string
			status  int
			body    string
			wantRaw json.RawMessage
			wantErr error
		}{
			{
				name:    "404のとき、状態欠落としてerrMissingGameStateを返す",
				status:  http.StatusNotFound,
				body:    `{"error":"game not found"}`,
				wantErr: errMissingGameState,
			},
			{
				name:    "200のとき、bodyをrawとして返す",
				status:  http.StatusOK,
				body:    `{"phase":"main"}`,
				wantRaw: json.RawMessage(`{"phase":"main"}`),
			},
		}

		for _, tc := range statusCases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL, &http.Client{})

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
				name:    "400の構造化エラーのとき、messageをエラー文に含める",
				status:  http.StatusBadRequest,
				body:    `{"error":"invalid player"}`,
				wantMsg: "invalid player",
			},
			{
				name:    "500の非JSONエラーのとき、ステータスコードとbody文字列を含める",
				status:  http.StatusInternalServerError,
				body:    "boom",
				wantMsg: "battle server returned 500: boom",
			},
		}

		for _, tc := range errorCases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL, &http.Client{})

				got, err := c.GetGameStateForPlayer(context.Background(), "game-1", 1)
				require.Nil(t, got)
				require.EqualError(t, err, tc.wantMsg)
			})
		}
	})
}

func TestBattleClient_GetTurnControlsForPlayer(t *testing.T) {
	t.Run("[対戦連携]turn controlsの不在判定", func(t *testing.T) {
		cases := []struct {
			name    string
			status  int
			body    string
			wantRaw json.RawMessage
		}{
			{
				name:    "null bodyのとき、不在としてnilを返す",
				status:  http.StatusOK,
				body:    "null",
				wantRaw: nil,
			},
			{
				name:    "空bodyのとき、不在としてnilを返す",
				status:  http.StatusOK,
				body:    "",
				wantRaw: nil,
			},
			{
				name:    "404のとき、不在としてnilを返す",
				status:  http.StatusNotFound,
				body:    "",
				wantRaw: nil,
			},
			{
				name:    "controlsが存在するとき、rawを返す",
				status:  http.StatusOK,
				body:    `{"controls":["end_turn"]}`,
				wantRaw: json.RawMessage(`{"controls":["end_turn"]}`),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL, &http.Client{})

				got, err := c.GetTurnControlsForPlayer(context.Background(), "game-1", 1)
				require.NoError(t, err)
				require.Equal(t, tc.wantRaw, got)
			})
		}
	})
}

func TestBattleClient_ListNpcModels(t *testing.T) {
	t.Run("[対戦連携]NPCモデル一覧取得のステータス処理", func(t *testing.T) {
		t.Run("battleのエンドポイントにリクエストが届かず404が返ったとき、NPCモデル一覧の欠落エラーを返す", func(t *testing.T) {
			srv := newBattleServer(t, http.StatusNotFound, `{"error":"not configured"}`)
			c := NewBattleClient(srv.URL, &http.Client{})

			got, err := c.ListNpcModels(context.Background())

			require.ErrorIs(t, err, errMissingNpcModels)
			require.Nil(t, got)
		})

		t.Run("battleのエンドポイントにリクエストが届いたとき、モデル一覧を返す", func(t *testing.T) {
			srv := newBattleServer(t, http.StatusOK, `{"models":[{"model":"TST-NPC-A","display_name":"Test NPC A","faction":"SHE","difficulty":"normal"}]}`)
			c := NewBattleClient(srv.URL, &http.Client{})

			got, err := c.ListNpcModels(context.Background())

			require.NoError(t, err)
			require.Equal(t, []NpcModelEntry{{Model: "TST-NPC-A", DisplayName: "Test NPC A", Faction: "SHE", Difficulty: "normal"}}, got)
		})
	})
}

func TestBattleClient_PostErrorResponse(t *testing.T) {
	t.Run("[対戦連携]送信系の非200応答のエラー変換", func(t *testing.T) {
		cases := []struct {
			name    string
			status  int
			body    string
			wantMsg string
			call    func(context.Context, BattleClient) error
		}{
			{
				name:    "アクション送信が400の構造化エラーを受けたとき、messageをエラー文に含める",
				status:  http.StatusBadRequest,
				body:    `{"error":"invalid action"}`,
				wantMsg: "invalid action",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.ProcessAction(ctx, "game-1", 1, "play_card", json.RawMessage(`{}`))
					return err
				},
			},
			{
				name:    "アクション送信が503の非JSONエラーを受けたとき、ステータスコードとbody文字列を含める",
				status:  http.StatusServiceUnavailable,
				body:    "service unavailable",
				wantMsg: "battle server returned 503: service unavailable",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.ProcessAction(ctx, "game-1", 1, "play_card", json.RawMessage(`{}`))
					return err
				},
			},
			{
				name:    "NPC対戦の作成が404の構造化エラーを受けたとき、messageをエラー文に含める",
				status:  http.StatusNotFound,
				body:    `{"error":"npc model not found"}`,
				wantMsg: "npc model not found",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.StartNPCBattle(ctx, nil, DeckInitiatives{}, "npc-1", PlayerSummaryRequest{}, PlayerSummaryRequest{})
					return err
				},
			},
			{
				name:    "NPC対戦の作成が500の非JSONエラーを受けたとき、ステータスコードとbody文字列を含める",
				status:  http.StatusInternalServerError,
				body:    "boom",
				wantMsg: "battle server returned 500: boom",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.StartNPCBattle(ctx, nil, DeckInitiatives{}, "npc-1", PlayerSummaryRequest{}, PlayerSummaryRequest{})
					return err
				},
			},
			{
				name:    "PvP対戦の作成が400の構造化エラーを受けたとき、messageをエラー文に含める",
				status:  http.StatusBadRequest,
				body:    `{"error":"deck mismatch"}`,
				wantMsg: "deck mismatch",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.CreatePvPGame(ctx, nil, nil, DeckInitiatives{}, DeckInitiatives{}, PlayerSummaryRequest{}, PlayerSummaryRequest{})
					return err
				},
			},
			{
				name:    "PvP対戦の作成がerrorフィールドが空の応答を受けたとき、ステータスコードとbody文字列を含める",
				status:  http.StatusBadGateway,
				body:    `{"error":""}`,
				wantMsg: `battle server returned 502: {"error":""}`,
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.CreatePvPGame(ctx, nil, nil, DeckInitiatives{}, DeckInitiatives{}, PlayerSummaryRequest{}, PlayerSummaryRequest{})
					return err
				},
			},
			{
				name:    "NPCターンの進行が409の構造化エラーを受けたとき、messageをエラー文に含める",
				status:  http.StatusConflict,
				body:    `{"error":"no pending npc turn"}`,
				wantMsg: "no pending npc turn",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.AdvanceNpcTurn(ctx, "game-1")
					return err
				},
			},
			{
				name:    "NPCターンの進行が500の非JSONエラーを受けたとき、ステータスコードとbody文字列を含める",
				status:  http.StatusInternalServerError,
				body:    "advance failed",
				wantMsg: "battle server returned 500: advance failed",
				call: func(ctx context.Context, c BattleClient) error {
					_, err := c.AdvanceNpcTurn(ctx, "game-1")
					return err
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newBattleServer(t, tc.status, tc.body)
				c := NewBattleClient(srv.URL, &http.Client{})

				err := tc.call(context.Background(), c)

				require.EqualError(t, err, tc.wantMsg)
			})
		}
	})
}
