package specdrift

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverMessageTypeCoverageExceptions は現時点でテストのどこからも観測されていない
// WSServerMsg* 定数の例外リスト。値は serverMessageTypeConstants が返す契約値
// (snake_case) を使う。対応が終わった種別はこのリストからも削除すること。
var serverMessageTypeCoverageExceptions = map[string]string{
	"game_state_restore": "Go 側に送信箇所が無く未使用 (issue #179)",
	"npc_battle_created": "npc_battle_start dispatch の実観測テストは issue #85 の別 PR で追加予定、未マージ",
}

func TestWSServerMessageTypeCoverage(t *testing.T) {
	t.Run("サーバー送出メッセージ種別の網羅観測", func(t *testing.T) {
		consts := serverMessageTypeConstants(t)
		tested := identifiersReferencedInTestFiles(t, "WSServerMsg")

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

// serverMessageTypeConstant は packages/ws-constants で宣言された 1 個の
// WSServerMsg* 定数を識別子と契約値のペアで表す。
type serverMessageTypeConstant struct {
	identifier string
	value      string
}

// serverMessageTypeConstants は packages/ws-constants の Go ソースを AST 解析し、
// 宣言されている全 WSServerMsg* 定数を識別子と契約値のペアで返す。
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

// identifiersReferencedInTestFiles は internal/ 配下の全 *_test.go を走査し、
// prefix から始まる識別子として参照されている語の集合を返す。パッケージの
// import エイリアスに依存しないよう、識別子の裸の語だけを照合する。
func identifiersReferencedInTestFiles(t *testing.T, prefix string) map[string]struct{} {
	t.Helper()
	pattern := regexp.MustCompile(`\b` + prefix + `\w+\b`)
	found := make(map[string]struct{})

	root := filepath.Join(repoRoot(t), "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, m := range pattern.FindAllString(string(src), -1) {
			found[m] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err)
	return found
}
