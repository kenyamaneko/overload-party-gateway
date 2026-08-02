// Package wsconstgen は packages/ws-constants の Go 定数を読み取り、
// npm パッケージ向けの TypeScript 定義を組み立てる。
package wsconstgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// 送信方向は定数名の接頭辞で判別する。
const (
	serverConstPrefix = "WSServerMsg"
	clientConstPrefix = "WSClientMsg"
)

// MessageTypes は WS メッセージ種別を送信方向ごとに保持する。
type MessageTypes struct {
	Server []string
	Client []string
}

// ParseMessageTypes は Go ソース中の WS メッセージ種別定数を、宣言順のまま送信方向ごとに抽出する。
// 送信方向を判別できない定数・文字列リテラル以外を値に持つ定数・いずれかの送信方向が空になる入力はエラーを返す。
func ParseMessageTypes(src []byte) (MessageTypes, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "constants.go", src, 0)
	if err != nil {
		return MessageTypes{}, fmt.Errorf("wsconstgen: parse source: %w", err)
	}

	var types MessageTypes
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				return MessageTypes{}, fmt.Errorf("wsconstgen: unexpected spec %T in a const declaration", spec)
			}
			if err := types.appendSpec(valueSpec); err != nil {
				return MessageTypes{}, err
			}
		}
	}

	if len(types.Server) == 0 {
		return MessageTypes{}, fmt.Errorf("wsconstgen: no constant with the %s prefix", serverConstPrefix)
	}
	if len(types.Client) == 0 {
		return MessageTypes{}, fmt.Errorf("wsconstgen: no constant with the %s prefix", clientConstPrefix)
	}
	return types, nil
}

func (t *MessageTypes) appendSpec(spec *ast.ValueSpec) error {
	if len(spec.Values) != len(spec.Names) {
		return fmt.Errorf("wsconstgen: constant %s has no value of its own", spec.Names[0].Name)
	}
	for i, name := range spec.Names {
		value, err := stringLiteral(spec.Values[i])
		if err != nil {
			return fmt.Errorf("wsconstgen: constant %s: %w", name.Name, err)
		}
		switch {
		case strings.HasPrefix(name.Name, serverConstPrefix):
			t.Server = append(t.Server, value)
		case strings.HasPrefix(name.Name, clientConstPrefix):
			t.Client = append(t.Client, value)
		default:
			return fmt.Errorf("wsconstgen: constant %s has neither the %s nor the %s prefix, so its direction is unknown",
				name.Name, serverConstPrefix, clientConstPrefix)
		}
	}
	return nil
}

func stringLiteral(expr ast.Expr) (string, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("value is not a string literal")
	}
	unquoted, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", fmt.Errorf("unquote string literal: %w", err)
	}
	return unquoted, nil
}
