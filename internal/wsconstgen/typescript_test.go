package wsconstgen

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const generatedHeaderText = "// Code generated from packages/ws-constants/constants.go; DO NOT EDIT."

var (
	constDeclPattern = regexp.MustCompile(`(?m)^export const (\w+) = (\[.*\]) as const;$`)
	typeDeclPattern  = regexp.MustCompile(`(?m)^export type (\w+) = \(typeof (\w+)\)\[number\];$`)
)

// decodeArrayValues は生成結果から指定した配列宣言を見つけ、その要素をJSON配列として
// 解釈した値を返す。エスケープが正しく構文的に妥当な配列リテラルであることを、
// 実際のパーサ(encoding/json)を通して確認する。
func decodeArrayValues(t *testing.T, out, arrayConstName string) []string {
	t.Helper()

	for _, m := range constDeclPattern.FindAllStringSubmatch(out, -1) {
		if m[1] != arrayConstName {
			continue
		}
		var values []string
		require.NoErrorf(t, json.Unmarshal([]byte(m[2]), &values),
			"配列宣言 %q の要素をJSON配列として解釈できない: %s", arrayConstName, m[2])
		return values
	}
	require.Failf(t, "配列宣言が生成結果に見つからない", "arrayConstName=%q", arrayConstName)
	return nil
}

func declaredConstNames(t *testing.T, out string) []string {
	t.Helper()
	var names []string
	for _, m := range constDeclPattern.FindAllStringSubmatch(out, -1) {
		names = append(names, m[1])
	}
	return names
}

func declaredTypeNames(t *testing.T, out string) []string {
	t.Helper()
	var names []string
	for _, m := range typeDeclPattern.FindAllStringSubmatch(out, -1) {
		names = append(names, m[1])
	}
	return names
}

func declarationIndex(t *testing.T, out string, pattern *regexp.Regexp, name string) int {
	t.Helper()
	for _, loc := range pattern.FindAllStringSubmatchIndex(out, -1) {
		if out[loc[2]:loc[3]] == name {
			return loc[0]
		}
	}
	require.Failf(t, "宣言が生成結果に見つからない", "name=%q", name)
	return -1
}

func referencedConstName(t *testing.T, out, typeName string) string {
	t.Helper()
	for _, m := range typeDeclPattern.FindAllStringSubmatch(out, -1) {
		if m[1] == typeName {
			return m[2]
		}
	}
	require.Failf(t, "型宣言が生成結果に見つからない", "typeName=%q", typeName)
	return ""
}

func TestRenderTypeScript(t *testing.T) {
	t.Run("[コード生成]サーバー向け・クライアント向けメッセージ種別一覧からのコード生成", func(t *testing.T) {
		t.Run("サーバー向け・クライアント向けそれぞれのメッセージ種別一覧を渡すと、それぞれについて値を並べた配列の宣言と、その配列の要素だけを取り得る型の宣言が生成される", func(t *testing.T) {
			out := RenderTypeScript(MessageTypes{
				Server: []string{"server_alpha"},
				Client: []string{"client_alpha"},
			})

			assert.Equal(t, []string{"WS_SERVER_MSG_TYPES", "WS_CLIENT_MSG_TYPES"}, declaredConstNames(t, out))
			assert.Equal(t, []string{"WSServerMsgType", "WSClientMsgType"}, declaredTypeNames(t, out))
		})

		t.Run("サーバー向け・クライアント向けそれぞれの配列宣言・型宣言は、サーバー向け・クライアント向けの順で、この順序のまま生成される", func(t *testing.T) {
			out := RenderTypeScript(MessageTypes{
				Server: []string{"server_alpha"},
				Client: []string{"client_alpha"},
			})

			idxServerArray := declarationIndex(t, out, constDeclPattern, "WS_SERVER_MSG_TYPES")
			idxServerType := declarationIndex(t, out, typeDeclPattern, "WSServerMsgType")
			idxClientArray := declarationIndex(t, out, constDeclPattern, "WS_CLIENT_MSG_TYPES")
			idxClientType := declarationIndex(t, out, typeDeclPattern, "WSClientMsgType")

			assert.Less(t, idxServerArray, idxServerType)
			assert.Less(t, idxServerType, idxClientArray)
			assert.Less(t, idxClientArray, idxClientType)
		})

		t.Run("生成される型宣言は、対応する送信方向の配列宣言を参照する", func(t *testing.T) {
			out := RenderTypeScript(MessageTypes{
				Server: []string{"server_alpha"},
				Client: []string{"client_alpha"},
			})

			assert.Equal(t, "WS_SERVER_MSG_TYPES", referencedConstName(t, out, "WSServerMsgType"))
			assert.Equal(t, "WS_CLIENT_MSG_TYPES", referencedConstName(t, out, "WSClientMsgType"))
		})

		t.Run("各方向の一覧内の並び順は、生成される配列内の要素の並び順にそのまま反映される", func(t *testing.T) {
			out := RenderTypeScript(MessageTypes{
				Server: []string{"server_zeta", "server_alpha"},
				Client: []string{"client_omega", "client_beta"},
			})

			assert.Equal(t, []string{"server_zeta", "server_alpha"}, decodeArrayValues(t, out, "WS_SERVER_MSG_TYPES"))
			assert.Equal(t, []string{"client_omega", "client_beta"}, decodeArrayValues(t, out, "WS_CLIENT_MSG_TYPES"))
		})

		t.Run("サーバー向けの一覧が空のとき、生成される配列は要素を持たない空の配列として書き出される", func(t *testing.T) {
			out := RenderTypeScript(MessageTypes{
				Server: []string{},
				Client: []string{"client_alpha"},
			})

			assert.Empty(t, decodeArrayValues(t, out, "WS_SERVER_MSG_TYPES"))
		})

		t.Run("クライアント向けの一覧が空のとき、生成される配列は要素を持たない空の配列として書き出される", func(t *testing.T) {
			out := RenderTypeScript(MessageTypes{
				Server: []string{"server_alpha"},
				Client: []string{},
			})

			assert.Empty(t, decodeArrayValues(t, out, "WS_CLIENT_MSG_TYPES"))
		})

		t.Run("一覧の件数が1件のときも複数件のときも、件数に応じた数の要素が配列に書き出される", func(t *testing.T) {
			testCases := []struct {
				name         string
				serverValues []string
				clientValues []string
			}{
				{
					name:         "サーバー向け・クライアント向けの件数がそれぞれ1件のとき",
					serverValues: []string{"server_alpha"},
					clientValues: []string{"client_alpha"},
				},
				{
					name:         "サーバー向け・クライアント向けの件数がそれぞれ複数件のとき",
					serverValues: []string{"server_alpha", "server_beta", "server_gamma"},
					clientValues: []string{"client_alpha", "client_beta"},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					out := RenderTypeScript(MessageTypes{
						Server: tc.serverValues,
						Client: tc.clientValues,
					})

					assert.Equal(t, tc.serverValues, decodeArrayValues(t, out, "WS_SERVER_MSG_TYPES"))
					assert.Equal(t, tc.clientValues, decodeArrayValues(t, out, "WS_CLIENT_MSG_TYPES"))
				})
			}
		})

		t.Run("生成される内容の先頭には、生成物であり手で編集してはいけないことを伝える見出しが、入力の内容によらず常に同じ文言で付く", func(t *testing.T) {
			testCases := []struct {
				name  string
				types MessageTypes
			}{
				{
					name: "サーバー向け・クライアント向けともに複数のメッセージ種別があるとき",
					types: MessageTypes{
						Server: []string{"server_alpha", "server_beta"},
						Client: []string{"client_alpha", "client_beta"},
					},
				},
				{
					name: "サーバー向け・クライアント向けともに1件ずつのメッセージ種別があるとき",
					types: MessageTypes{
						Server: []string{"server_only"},
						Client: []string{"client_only"},
					},
				},
				{
					name: "サーバー向け・クライアント向けともにメッセージ種別が無いとき",
					types: MessageTypes{
						Server: []string{},
						Client: []string{},
					},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					out := RenderTypeScript(tc.types)
					assert.True(t, strings.HasPrefix(out, generatedHeaderText), "生成結果の先頭が固定の見出しになっていない: %q", out)
				})
			}
		})

		t.Run("メッセージ種別の値に二重引用符やバックスラッシュのような特殊文字が含まれているとき、生成される配列の要素をJSON配列として解釈すると元の値と一致する", func(t *testing.T) {
			serverValues := []string{`say "hi"`, `back\slash`}
			out := RenderTypeScript(MessageTypes{
				Server: serverValues,
				Client: []string{"client_alpha"},
			})

			assert.Equal(t, serverValues, decodeArrayValues(t, out, "WS_SERVER_MSG_TYPES"))
		})
	})
}
