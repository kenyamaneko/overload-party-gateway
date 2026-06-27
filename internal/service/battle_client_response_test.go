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

// TestRawToMap は battle アクション data の変換が、入力の有無と不正データを区別する契約を検証する。
func TestRawToMap(t *testing.T) {
	cases := []struct {
		name    string
		raw     json.RawMessage
		wantMap map[string]interface{}
		wantErr bool
	}{
		{
			name:    "空入力は nil を返す",
			raw:     nil,
			wantMap: nil,
			wantErr: false,
		},
		{
			name:    "オブジェクトを map に変換する",
			raw:     json.RawMessage(`{"target":"TST-0001"}`),
			wantMap: map[string]interface{}{"target": "TST-0001"},
			wantErr: false,
		},
		{
			name:    "不正な JSON はエラーになる",
			raw:     json.RawMessage(`{`),
			wantMap: nil,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := rawToMap(tc.raw)
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, tc.wantMap, got)
		})
	}
}

// TestParseBattleError は battle エラーレスポンスから構造化メッセージを抽出し、抽出不能時はステータスと body にフォールバックする契約を検証する。
func TestParseBattleError(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantMsg    string
	}{
		{
			name:       "構造化された error フィールドを抽出する",
			statusCode: http.StatusBadRequest,
			body:       `{"error":"deck not found"}`,
			wantMsg:    "deck not found",
		},
		{
			name:       "error フィールドが空ならステータスと body にフォールバックする",
			statusCode: http.StatusBadRequest,
			body:       `{"error":""}`,
			wantMsg:    `battle server returned 400: {"error":""}`,
		},
		{
			name:       "非 JSON body はステータスと body にフォールバックする",
			statusCode: http.StatusBadGateway,
			body:       "upstream down",
			wantMsg:    "battle server returned 502: upstream down",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := parseBattleError(tc.statusCode, []byte(tc.body))
			require.EqualError(t, err, tc.wantMsg)
		})
	}
}

// TestBattleClient_GetGameStateForPlayer_StatusHandling は state 取得が 404 を不在 (nil) として扱い、200 を raw として返す契約を検証する。
func TestBattleClient_GetGameStateForPlayer_StatusHandling(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantRaw json.RawMessage
	}{
		{
			name:    "404 は不在として nil を返す",
			status:  http.StatusNotFound,
			body:    `{"error":"game not found"}`,
			wantRaw: nil,
		},
		{
			name:    "200 は body を raw として返す",
			status:  http.StatusOK,
			body:    `{"phase":"main"}`,
			wantRaw: json.RawMessage(`{"phase":"main"}`),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := newBattleServer(t, tc.status, tc.body)
			c := NewBattleClient(srv.URL)

			got, err := c.GetGameStateForPlayer(context.Background(), "game-1", 1)
			require.NoError(t, err)
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
			name:    "非 JSON エラーはステータスと body にフォールバックする",
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
