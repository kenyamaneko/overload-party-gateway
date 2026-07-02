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

// TestBattleClient_ProcessAction_RejectsInvalidData は不正なアクションデータが battle へ送信される前に弾かれる契約を検証する。
func TestBattleClient_ProcessAction_RejectsInvalidData(t *testing.T) {
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
}

// TestBattleClient_ProcessAction_SendsTransformedData はアクションデータが map へ変換され battle へ送られる契約を検証する。
func TestBattleClient_ProcessAction_SendsTransformedData(t *testing.T) {
	cases := []struct {
		name     string
		data     json.RawMessage
		wantData interface{}
	}{
		{
			name:     "空入力は data なしで送られる",
			data:     nil,
			wantData: nil,
		},
		{
			name:     "オブジェクトは map として送られる",
			data:     json.RawMessage(`{"target":"TST-0001"}`),
			wantData: map[string]interface{}{"target": "TST-0001"},
		},
	}

	for _, tc := range cases {
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
}

// TestBattleClient_Post_NonOKStatus_SurfacesBattleError は POST 系メソッドが battle の非 200 応答を構造化エラーメッセージへ変換して呼び出し元へ返す契約を検証する。
func TestBattleClient_Post_NonOKStatus_SurfacesBattleError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantMsg string
		call    func(context.Context, BattleClient) error
	}{
		{
			name:    "ProcessAction は battle の構造化エラーメッセージを surface する",
			status:  http.StatusBadRequest,
			body:    `{"error":"invalid action"}`,
			wantMsg: "invalid action",
			call: func(ctx context.Context, c BattleClient) error {
				_, err := c.ProcessAction(ctx, "game-1", 1, "play_card", json.RawMessage(`{}`))
				return err
			},
		},
		{
			name:    "StartNPCBattle は非 JSON エラーをステータスと body に変換する",
			status:  http.StatusInternalServerError,
			body:    "boom",
			wantMsg: "battle server returned 500: boom",
			call: func(ctx context.Context, c BattleClient) error {
				_, err := c.StartNPCBattle(ctx, nil, DeckInitiatives{}, "npc-1", PlayerSummaryRequest{}, PlayerSummaryRequest{})
				return err
			},
		},
		{
			name:    "CreatePvPGame は battle の構造化エラーメッセージを surface する",
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
}

// TestBattleClient_GetGameStateForPlayer_StatusHandling は state 取得のステータス別レスポンス変換契約を検証する。
func TestBattleClient_GetGameStateForPlayer_StatusHandling(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantRaw json.RawMessage
		wantErr error
	}{
		{
			name:    "404 は状態欠落として errMissingGameState を返す",
			status:  http.StatusNotFound,
			body:    `{"error":"game not found"}`,
			wantErr: errMissingGameState,
		},
		{
			name:    "200 は body を raw として返す",
			status:  http.StatusOK,
			body:    `{"phase":"main"}`,
			wantRaw: json.RawMessage(`{"phase":"main"}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newBattleServer(t, tc.status, tc.body)
			c := NewBattleClient(srv.URL)

			got, err := c.GetGameStateForPlayer(context.Background(), "game-1", 1)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantRaw, got)
		})
	}
}

// TestBattleClient_GetGameStateForPlayer_ErrorStatus は 200/404 以外のステータスが battle のエラーメッセージに変換されることを検証する。
func TestBattleClient_GetGameStateForPlayer_ErrorStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantMsg string
	}{
		{
			name:    "構造化エラーは message を surface する",
			status:  http.StatusBadRequest,
			body:    `{"error":"invalid player"}`,
			wantMsg: "invalid player",
		},
		{
			name:    "非 JSON エラーはステータスコードと body 文字列をそのままエラー文に含める",
			status:  http.StatusInternalServerError,
			body:    "boom",
			wantMsg: "battle server returned 500: boom",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := newBattleServer(t, tc.status, tc.body)
			c := NewBattleClient(srv.URL)

			got, err := c.GetGameStateForPlayer(context.Background(), "game-1", 1)
			require.Nil(t, got)
			require.EqualError(t, err, tc.wantMsg)
		})
	}
}

// TestBattleClient_GetTurnControlsForPlayer_EmptyHandling は turn controls の不在表現 (null / 空 body / 404) と存在を区別する契約を検証する。
func TestBattleClient_GetTurnControlsForPlayer_EmptyHandling(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantRaw json.RawMessage
	}{
		{
			name:    "null body は不在として nil を返す",
			status:  http.StatusOK,
			body:    "null",
			wantRaw: nil,
		},
		{
			name:    "空 body は不在として nil を返す",
			status:  http.StatusOK,
			body:    "",
			wantRaw: nil,
		},
		{
			name:    "404 は不在として nil を返す",
			status:  http.StatusNotFound,
			body:    "",
			wantRaw: nil,
		},
		{
			name:    "controls が存在すれば raw を返す",
			status:  http.StatusOK,
			body:    `{"controls":["end_turn"]}`,
			wantRaw: json.RawMessage(`{"controls":["end_turn"]}`),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := newBattleServer(t, tc.status, tc.body)
			c := NewBattleClient(srv.URL)

			got, err := c.GetTurnControlsForPlayer(context.Background(), "game-1", 1)
			require.NoError(t, err)
			require.Equal(t, tc.wantRaw, got)
		})
	}
}
