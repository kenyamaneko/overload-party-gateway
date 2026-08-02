package wsconstgen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMessageTypes(t *testing.T) {
	t.Run("WS メッセージ種別の抽出", func(t *testing.T) {
		t.Run("サーバー向けとクライアント向けの定数があるとき、宣言順のまま送信方向ごとに分かれる", func(t *testing.T) {
			src := []byte(`package ws

const (
	WSServerMsgAlpha = "alpha"
	WSServerMsgBravo = "bravo"
)

const (
	WSClientMsgCharlie = "charlie"
)
`)

			types, err := ParseMessageTypes(src)

			require.NoError(t, err)
			require.Equal(t, []string{"alpha", "bravo"}, types.Server)
			require.Equal(t, []string{"charlie"}, types.Client)
		})

		t.Run("定数以外の宣言が混ざっているとき、種別として取り込まれない", func(t *testing.T) {
			src := []byte(`package ws

var somethingElse = "delta"

const (
	WSServerMsgAlpha   = "alpha"
	WSClientMsgCharlie = "charlie"
)
`)

			types, err := ParseMessageTypes(src)

			require.NoError(t, err)
			require.Equal(t, []string{"alpha"}, types.Server)
			require.Equal(t, []string{"charlie"}, types.Client)
		})

		cases := []struct {
			name        string
			src         string
			wantMessage string
		}{
			{
				name: "送信方向を表す接頭辞が無い定数があるとき、方向を判別できない旨のエラーになる",
				src: `package ws

const (
	WSServerMsgAlpha   = "alpha"
	WSClientMsgCharlie = "charlie"
	SomethingElse      = "delta"
)
`,
				wantMessage: "constant SomethingElse has neither the WSServerMsg nor the WSClientMsg prefix",
			},
			{
				name: "値が文字列リテラルでない定数があるとき、文字列リテラルでない旨のエラーになる",
				src: `package ws

const (
	WSServerMsgAlpha   = 1
	WSClientMsgCharlie = "charlie"
)
`,
				wantMessage: "constant WSServerMsgAlpha: value is not a string literal",
			},
			{
				name: "値を省いた定数があるとき、値が無い旨のエラーになる",
				src: `package ws

const (
	WSServerMsgAlpha = "alpha"
	WSServerMsgBravo
	WSClientMsgCharlie = "charlie"
)
`,
				wantMessage: "constant WSServerMsgBravo has no value of its own",
			},
			{
				name: "サーバー向けの定数が 1 つも無いとき、サーバー向けが空である旨のエラーになる",
				src: `package ws

const (
	WSClientMsgCharlie = "charlie"
)
`,
				wantMessage: "no constant with the WSServerMsg prefix",
			},
			{
				name: "クライアント向けの定数が 1 つも無いとき、クライアント向けが空である旨のエラーになる",
				src: `package ws

const (
	WSServerMsgAlpha = "alpha"
)
`,
				wantMessage: "no constant with the WSClientMsg prefix",
			},
			{
				name:        "Go ソースとして解釈できないとき、解析に失敗した旨のエラーになる",
				src:         "this is not go source",
				wantMessage: "parse source",
			},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				_, err := ParseMessageTypes([]byte(c.src))

				require.ErrorContains(t, err, c.wantMessage)
			})
		}
	})
}
