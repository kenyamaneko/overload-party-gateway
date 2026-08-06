package specdrift

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverMessageTypeCoverageExceptions はテストで未観測の WSServerMsg* 定数の例外リスト。対応が終わった種別はこのリストからも削除すること。
var serverMessageTypeCoverageExceptions = map[string]string{
	"game_state_restore": "Go 側に送信箇所が無く未使用 (issue #179)",
	"npc_battle_created": "npc_battle_start dispatch の実観測テストは issue #85 の別ブランチで追加予定、未マージ。マージ後はこの行を削除して再実行し green を確認すること",
}

func TestWSServerMessageTypeCoverage(t *testing.T) {
	t.Run("サーバー送出メッセージ種別の網羅観測", func(t *testing.T) {
		consts := serverMessageTypeConstants(t)
		identifiers := make([]string, len(consts))
		for i, c := range consts {
			identifiers[i] = c.identifier
		}
		tested := serverMessageTypesObservedInTestFiles(t, identifiers)

		t.Run("定数が受信フレームとして観測されているか、理由付きで例外リストに存在する", func(t *testing.T) {
			for _, c := range consts {
				t.Run(c.value+" がテストで観測されているか例外リストに存在する", func(t *testing.T) {
					_, isTested := tested[c.identifier]
					_, isExcepted := serverMessageTypeCoverageExceptions[c.value]
					assert.Truef(t, isTested || isExcepted,
						"サーバー送出メッセージ種別 %q (%s) がどのテストにも観測されておらず、例外リストにも無い",
						c.value, c.identifier)
				})
			}
		})

		t.Run("例外リストの値が現存する定数と対応する", func(t *testing.T) {
			valueSet := make(map[string]struct{}, len(consts))
			for _, c := range consts {
				valueSet[c.value] = struct{}{}
			}
			for v := range serverMessageTypeCoverageExceptions {
				t.Run(v+" に対応する定数が現存する", func(t *testing.T) {
					_, ok := valueSet[v]
					assert.Truef(t, ok, "例外リストの値 %q に対応する WSServerMsg 定数が存在しない", v)
				})
			}
		})

		t.Run("例外リストの値が実際にはテストされていない", func(t *testing.T) {
			identifierOf := make(map[string]string, len(consts))
			for _, c := range consts {
				identifierOf[c.value] = c.identifier
			}
			for v, reason := range serverMessageTypeCoverageExceptions {
				t.Run(v+" は実際にテストされていない", func(t *testing.T) {
					identifier, ok := identifierOf[v]
					require.True(t, ok)
					_, isTested := tested[identifier]
					assert.Falsef(t, isTested,
						"例外リストの値 %q (%s) は既にテストで観測されているので例外リストから削除する (登録理由: %s)",
						v, identifier, reason)
				})
			}
		})
	})
}

// serverMessageTypeConstant は WSServerMsg* 定数 1 個の識別子と契約値のペア。
type serverMessageTypeConstant struct {
	identifier string
	value      string
}

// serverMessageTypeConstants は ws-constants の Go ソースを AST 解析し、全 WSServerMsg* 定数を返す。
func serverMessageTypeConstants(t *testing.T) []serverMessageTypeConstant {
	t.Helper()
	srcPath := filepath.Join(repoRoot(t), "packages", "ws-constants", "constants.go")
	src, err := os.ReadFile(srcPath)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "constants.go", src, 0)
	require.NoError(t, err)

	var consts []serverMessageTypeConstant
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			require.True(t, ok)
			for i, name := range valueSpec.Names {
				if !strings.HasPrefix(name.Name, "WSServerMsg") {
					continue
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				require.Truef(t, ok, "constant %s has no string literal value", name.Name)
				value, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				consts = append(consts, serverMessageTypeConstant{identifier: name.Name, value: value})
			}
		}
	}
	require.NotEmpty(t, consts, "WSServerMsg 接頭辞の定数が constants.go に見つからない")
	return consts
}

// serverMessageTypesObservedInTestFiles は *_test.go を AST 解析し、knownIdentifiers のうち
// assert.Equal 系 / readUntil* 系呼び出しで実際に使われているものを返す (コメント等の文字列一致は対象外)。
// readUntilActionPerformed のように種別がヘルパー名自体に埋め込まれる場合は関数名の末尾一致でも判定するが、
// これはヘルパー名からの推定であり type を実際に assert している保証は無い。
func serverMessageTypesObservedInTestFiles(t *testing.T, knownIdentifiers []string) map[string]struct{} {
	t.Helper()
	observed := make(map[string]struct{})
	fset := token.NewFileSet()

	root := filepath.Join(repoRoot(t), "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		file, err := parser.ParseFile(fset, path, src, 0)
		require.NoError(t, err)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calleeName(call.Fun)
			if name == "" || !isObservationCallName(name) {
				return true
			}
			for _, arg := range call.Args {
				ast.Inspect(arg, func(m ast.Node) bool {
					if argName := identifierName(m); strings.HasPrefix(argName, "WSServerMsg") {
						observed[argName] = struct{}{}
					}
					return true
				})
			}
			if strings.HasPrefix(name, "readUntil") {
				suffix := strings.TrimPrefix(name, "readUntil")
				for _, ident := range knownIdentifiers {
					if suffix != "" && strings.HasSuffix(ident, suffix) {
						observed[ident] = struct{}{}
					}
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	return observed
}

// isObservationCallName は呼び出しが「受信フレームの type を検証する」意図かを判定する。
func isObservationCallName(name string) bool {
	return strings.Contains(name, "Equal") || strings.HasPrefix(name, "readUntil")
}

// calleeName は呼び出し式から関数名 (パッケージ修飾子を除く) を取り出す。
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	default:
		return ""
	}
}

// identifierName は式が単純な識別子またはセレクタ式であれば、その裸の名前を返す。
func identifierName(n ast.Node) string {
	switch e := n.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}
