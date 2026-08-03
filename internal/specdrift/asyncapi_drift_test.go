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

func TestAsyncAPIChannelDrift(t *testing.T) {
	t.Run("asyncapi.yaml の channel address と ws-constants の整合", func(t *testing.T) {
		// SSoT は asyncapi.yaml。channel が増減したら対応する定数を ws-constants に追加 / 削除しないと落ちる。
		// payload-less な "型のみ" のメッセージ (game_state / pong など battle 由来 raw JSON だけを
		// envelope に積むタイプ) は asyncapi.yaml に現れないため検証対象外とし、ws-constants 側でのみ持つ。
		spec := loadAsyncAPISpec(t)
		channels, ok := spec["channels"].(map[string]interface{})
		require.True(t, ok, "channels が見つからない")

		allConsts := wsMessageTypeConstants()
		for _, ch := range channels {
			entry, ok := ch.(map[string]interface{})
			require.True(t, ok)
			addr, ok := entry["address"].(string)
			require.True(t, ok, "channel に address が無い")

			t.Run(addr+" が ws-constants の定数に存在する", func(t *testing.T) {
				require.Containsf(t, allConsts, addr,
					"asyncapi.yaml の channel address %q が ws-constants の定数に存在しない", addr)
			})
		}
	})
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
		genws.WSServerMsgServerUpdate,
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
