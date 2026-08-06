// Package specdrift verifies that data/{openapi,asyncapi}.yaml と Go 側の
// 派生定数 (packages/ws-constants など) が drift していないことを保証する。
package specdrift

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/kenyamaneko/overload-party-gateway/internal/wsconstgen"
)

// rawJSONPassthroughTypes は battle 由来の raw JSON をそのまま envelope の data に積むだけで
// 独自スキーマを持たないメッセージ種別。asyncapi.yaml にスキーマとして記述しようがないため
// 双方向突合の対象から除外する。
// game_state_restore は Go 側に送信箇所が無く未使用だが、issue #179 で切り分けが済むまで本カテゴリに残す。
var rawJSONPassthroughTypes = map[string]struct{}{
	"game_state":         {},
	"turn_controls":      {},
	"game_state_restore": {},
}

// undocumentedControlMessageTypes は実際に送受信されているが asyncapi.yaml に未記載のメッセージ種別。
// data/asyncapi.yaml の更新は packages/api-gateway の生成コード再生成を伴うため本テストの対応範囲外とし、
// issue #179 で追跡する。対応が終わった種別はこのリストからも削除すること。
var undocumentedControlMessageTypes = map[string]struct{}{
	"game_entered":          {},
	"matchmaking_started":   {},
	"matchmaking_cancelled": {},
	"matchmaking_cancel":    {},
	"ping":                  {},
	"pong":                  {},
}

func TestAsyncAPIChannelDrift(t *testing.T) {
	t.Run("asyncapi.yaml と ws-constants の WS メッセージ種別の整合", func(t *testing.T) {
		spec := loadAsyncAPISpec(t)
		channels, ok := spec["channels"].(map[string]interface{})
		require.True(t, ok, "channels が見つからない")

		asyncAPIAddresses := make(map[string]struct{}, len(channels))
		for _, ch := range channels {
			entry, ok := ch.(map[string]interface{})
			require.True(t, ok)
			addr, ok := entry["address"].(string)
			require.True(t, ok, "channel に address が無い")
			asyncAPIAddresses[addr] = struct{}{}
		}

		wsConsts := wsMessageTypeConstants(t)
		wsConstSet := make(map[string]struct{}, len(wsConsts))
		for _, v := range wsConsts {
			wsConstSet[v] = struct{}{}
		}

		t.Run("asyncapi.yaml の channel address が ws-constants の定数に存在する", func(t *testing.T) {
			for addr := range asyncAPIAddresses {
				t.Run(addr+" が ws-constants の定数に存在する", func(t *testing.T) {
					_, ok := wsConstSet[addr]
					require.Truef(t, ok,
						"asyncapi.yaml の channel address %q が ws-constants の定数に存在しない", addr)
				})
			}
		})

		t.Run("ws-constants の定数が asyncapi.yaml か例外リストのいずれかに存在する", func(t *testing.T) {
			for _, v := range wsConsts {
				t.Run(v+" が asyncapi.yaml か例外リストのいずれかに存在する", func(t *testing.T) {
					_, inSpec := asyncAPIAddresses[v]
					_, rawJSON := rawJSONPassthroughTypes[v]
					_, undocumented := undocumentedControlMessageTypes[v]
					require.Truef(t, inSpec || rawJSON || undocumented,
						"ws-constants の定数値 %q が asyncapi.yaml の channel にも例外リストにも存在しない", v)
				})
			}
		})

		t.Run("例外リストの値が ws-constants の定数として現存する", func(t *testing.T) {
			for v := range rawJSONPassthroughTypes {
				t.Run(v+" に対応する ws-constants の定数が現存する", func(t *testing.T) {
					_, ok := wsConstSet[v]
					require.Truef(t, ok,
						"例外リスト rawJSONPassthroughTypes の値 %q に対応する ws-constants の定数が存在しない", v)
				})
			}
			for v := range undocumentedControlMessageTypes {
				t.Run(v+" に対応する ws-constants の定数が現存する", func(t *testing.T) {
					_, ok := wsConstSet[v]
					require.Truef(t, ok,
						"例外リスト undocumentedControlMessageTypes の値 %q に対応する ws-constants の定数が存在しない", v)
				})
			}
		})

		t.Run("例外リストの値が asyncapi.yaml に記載されていない", func(t *testing.T) {
			for v := range rawJSONPassthroughTypes {
				t.Run(v+" は asyncapi.yaml に記載されていない", func(t *testing.T) {
					_, inSpec := asyncAPIAddresses[v]
					require.Falsef(t, inSpec,
						"例外リスト rawJSONPassthroughTypes の値 %q は asyncapi.yaml に記載済みなので例外リストから削除する", v)
				})
			}
			for v := range undocumentedControlMessageTypes {
				t.Run(v+" は asyncapi.yaml に記載されていない", func(t *testing.T) {
					_, inSpec := asyncAPIAddresses[v]
					require.Falsef(t, inSpec,
						"例外リスト undocumentedControlMessageTypes の値 %q は asyncapi.yaml に記載済みなので例外リストから削除する", v)
				})
			}
		})
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

// wsMessageTypeConstants は packages/ws-constants の Go ソースを AST 解析し、
// 宣言されている全 WS メッセージ種別の値を返す。
func wsMessageTypeConstants(t *testing.T) []string {
	t.Helper()
	srcPath := filepath.Join(repoRoot(t), "packages", "ws-constants", "constants.go")
	src, err := os.ReadFile(srcPath)
	require.NoError(t, err)
	types, err := wsconstgen.ParseMessageTypes(src)
	require.NoError(t, err)
	return append(append([]string{}, types.Server...), types.Client...)
}
