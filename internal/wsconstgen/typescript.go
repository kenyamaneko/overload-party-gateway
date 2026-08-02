package wsconstgen

import (
	"fmt"
	"strings"
)

// 呼び出し方 (相対パス / 絶対パス) で生成物が変わらないよう、見出しの出典は固定文言にする。
const generatedHeader = "// Code generated from packages/ws-constants/constants.go; DO NOT EDIT.\n\n"

// RenderTypeScript は WS メッセージ種別を npm パッケージが公開する TypeScript 定義に整形する。
func RenderTypeScript(types MessageTypes) string {
	var out strings.Builder
	out.WriteString(generatedHeader)
	writeUnion(&out, "WS_SERVER_MSG_TYPES", "WSServerMsgType", types.Server)
	out.WriteString("\n")
	writeUnion(&out, "WS_CLIENT_MSG_TYPES", "WSClientMsgType", types.Client)
	return out.String()
}

func writeUnion(out *strings.Builder, constName, typeName string, values []string) {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	fmt.Fprintf(out, "export const %s = [%s] as const;\n", constName, strings.Join(quoted, ", "))
	fmt.Fprintf(out, "export type %s = (typeof %s)[number];\n", typeName, constName)
}
