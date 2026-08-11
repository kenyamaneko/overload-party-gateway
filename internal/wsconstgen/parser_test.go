package wsconstgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMessageTypes(t *testing.T) {
	t.Run("[コード生成]Goの定数宣言からのWebSocketメッセージ種別抽出と送信方向分類", func(t *testing.T) {
		t.Run("サーバー方向・クライアント方向のそれぞれに定数が1件だけあるとき、それぞれ1件の一覧として返る", func(t *testing.T) {
			src := []byte(`package foo
const (
	WSServerMsgOnly = "server_only"
	WSClientMsgOnly = "client_only"
)
`)
			got, err := ParseMessageTypes(src)
			require.NoError(t, err)
			assert.Equal(t, []string{"server_only"}, got.Server)
			assert.Equal(t, []string{"client_only"}, got.Client)
		})

		t.Run("サーバー方向・クライアント方向のそれぞれに定数が複数件あるとき、それぞれソースに現れた順序を保った複数件の一覧として返る", func(t *testing.T) {
			src := []byte(`package foo
const (
	WSServerMsgZeta = "server_zeta"
	WSServerMsgAlpha = "server_alpha"
	WSClientMsgOmega = "client_omega"
	WSClientMsgBeta = "client_beta"
)
`)
			got, err := ParseMessageTypes(src)
			require.NoError(t, err)
			assert.Equal(t, []string{"server_zeta", "server_alpha"}, got.Server)
			assert.Equal(t, []string{"client_omega", "client_beta"}, got.Client)
		})

		t.Run("一つの宣言に複数の定数名がまとめて書かれていて、それぞれが異なる送信方向の接頭辞を持つとき、それぞれが対応する方向の一覧に正しく振り分けられる", func(t *testing.T) {
			src := []byte(`package foo
const WSServerMsgFoo, WSClientMsgBar = "server_foo", "client_bar"
`)
			got, err := ParseMessageTypes(src)
			require.NoError(t, err)
			assert.Equal(t, []string{"server_foo"}, got.Server)
			assert.Equal(t, []string{"client_bar"}, got.Client)
		})

		t.Run("定数宣言以外の宣言が混在していても、それらは無視され定数だけが分類対象になる", func(t *testing.T) {
			src := []byte(`package foo

var unrelated = 1

func doStuff() {}

const (
	WSServerMsgFoo = "server_foo"
	WSClientMsgBar = "client_bar"
)
`)
			got, err := ParseMessageTypes(src)
			require.NoError(t, err)
			assert.Equal(t, []string{"server_foo"}, got.Server)
			assert.Equal(t, []string{"client_bar"}, got.Client)
		})

		t.Run("定数の名前が、サーバー方向・クライアント方向いずれの接頭辞にも当てはまらないとき、エラーになり、エラーの内容からどの定数名が原因かが分かる", func(t *testing.T) {
			src := []byte(`package foo
const (
	WSServerMsgFoo = "server_foo"
	WSClientMsgBar = "client_bar"
	WSFooMsgBaz = "unknown_baz"
)
`)
			_, err := ParseMessageTypes(src)
			require.Error(t, err)
			assert.ErrorContains(t, err, "WSFooMsgBaz")
		})

		t.Run("定数の値が(二重引用符で囲んだ)文字列リテラルでなく、数値や式、他の定数の参照などであるとき、エラーになり、エラーの内容からどの定数の値が原因かが分かる", func(t *testing.T) {
			testCases := []struct {
				name               string
				offendingConstDecl string
				offendingConstName string
			}{
				{
					name:               "定数の値が数値であるとき",
					offendingConstDecl: `WSServerMsgNum = 123`,
					offendingConstName: "WSServerMsgNum",
				},
				{
					name:               "定数の値が式であるとき",
					offendingConstDecl: `WSServerMsgExpr = "a" + "b"`,
					offendingConstName: "WSServerMsgExpr",
				},
				{
					name:               "定数の値が他の定数の参照であるとき",
					offendingConstDecl: `WSServerMsgRef = WSServerMsgFoo`,
					offendingConstName: "WSServerMsgRef",
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					src := []byte(`package foo
const (
	WSServerMsgFoo = "server_foo"
	WSClientMsgBar = "client_bar"
	` + tc.offendingConstDecl + `
)
`)
					_, err := ParseMessageTypes(src)
					require.Error(t, err)
					assert.ErrorContains(t, err, tc.offendingConstName)
				})
			}
		})

		t.Run("定数が独自の値を持たず、直前の宣言の値を暗黙的に引き継ぐ形で宣言されているとき、エラーになり、エラーの内容からどの定数が原因かが分かる", func(t *testing.T) {
			src := []byte(`package foo
const (
	WSServerMsgFoo = "server_foo"
	WSServerMsgBar
	WSClientMsgBaz = "client_baz"
)
`)
			_, err := ParseMessageTypes(src)
			require.Error(t, err)
			assert.ErrorContains(t, err, "WSServerMsgBar")
		})

		t.Run("Goのソースとして解析できない入力を渡したとき、エラーになる", func(t *testing.T) {
			src := []byte(`this is not valid go source {{{`)
			_, err := ParseMessageTypes(src)
			assert.Error(t, err)
		})

		t.Run("サーバー方向に分類される定数が一つも無いとき、エラーになり、エラーの内容からサーバー方向が空であることが分かる", func(t *testing.T) {
			src := []byte(`package foo
const (
	WSClientMsgBar = "client_bar"
)
`)
			_, err := ParseMessageTypes(src)
			require.Error(t, err)
			assert.ErrorContains(t, err, "WSServerMsg")
		})

		t.Run("サーバー方向には定数があるが、クライアント方向に分類される定数が一つも無いとき、エラーになり、エラーの内容からクライアント方向が空であることが分かる", func(t *testing.T) {
			src := []byte(`package foo
const (
	WSServerMsgFoo = "server_foo"
)
`)
			_, err := ParseMessageTypes(src)
			require.Error(t, err)
			assert.ErrorContains(t, err, "WSClientMsg")
		})
	})
}
