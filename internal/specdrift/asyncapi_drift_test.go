// Package specdrift verifies that data/{openapi,asyncapi}.yaml と Go 側の
// 派生定数 (packages/ws-constants など) が drift していないことを保証する。
package specdrift

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// TestAsyncAPIChannelDrift は data/asyncapi.yaml が宣言する channel address (=
// WS message type 値) が ws-constants 側に同値の定数として存在することを検証する。
// asyncapi.yaml の channel が増減した場合、対応する定数を ws-constants に
// 追加 / 削除しないと本テストが落ちる。
//
// asyncapi.yaml に存在しない (payload-less) "型のみ" のメッセージ
// (game_state / pong など battle 由来 raw JSON だけを envelope に積むタイプ)
// は検証対象外とし、ws-constants 側でのみ持つ。
func TestAsyncAPIChannelDrift(t *testing.T) {
	spec := loadAsyncAPISpec(t)
	channels, ok := spec["channels"].(map[string]interface{})
	require.True(t, ok, "channels が見つからない")

	declared := make([]string, 0, len(channels))
	for _, ch := range channels {
		entry, ok := ch.(map[string]interface{})
		require.True(t, ok)
		addr, ok := entry["address"].(string)
		require.True(t, ok, "channel に address が無い")
		declared = append(declared, addr)
	}

	allConsts := wsMessageTypeConstants()
	for _, addr := range declared {
		require.Containsf(t, allConsts, addr,
			"asyncapi.yaml の channel address %q が ws-constants の定数に存在しない", addr)
	}
}

// loadAsyncAPISpec は data/asyncapi.yaml をパースして返す。
func loadAsyncAPISpec(t *testing.T) map[string]interface{} {
	t.Helper()
	specPath := filepath.Join(repoRoot(t), "data", "asyncapi.yaml")
	raw, err := os.ReadFile(specPath)
	require.NoError(t, err)
	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	return doc
}

// repoRoot は本ファイルから見たリポジトリルートを返す (internal/specdrift/ から 2 階層上)。
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..")
}

// wsMessageTypeConstants は ws-constants の全 type 値を string slice で返す。
// 新しい定数を追加した場合、本関数にも追記すること (drift test の網羅性を維持するため)。
func wsMessageTypeConstants() []string {
	return []string{
		// server
		genws.WSServerMsgGameState,
		genws.WSServerMsgGameOver,
		genws.WSServerMsgError,
		genws.WSServerMsgGameEntered,
		genws.WSServerMsgMatchmakingStarted,
		genws.WSServerMsgMatchmakingCancelled,
		genws.WSServerMsgActionRejected,
		genws.WSServerMsgStampUsed,
		genws.WSServerMsgPong,
		genws.WSServerMsgMatchFound,
		genws.WSServerMsgActionPerformed,
		genws.WSServerMsgTurnControls,
		genws.WSServerMsgNpcBattleCreated,
		genws.WSServerMsgOpponentDisconnected,
		genws.WSServerMsgOpponentReconnected,
		genws.WSServerMsgGameStateRestore,
		// client
		genws.WSClientMsgGameEnter,
		genws.WSClientMsgMatchmakingStart,
		genws.WSClientMsgMatchmakingCancel,
		genws.WSClientMsgGameAction,
		genws.WSClientMsgUseStamp,
		genws.WSClientMsgPing,
		genws.WSClientMsgNpcBattleStart,
	}
}
