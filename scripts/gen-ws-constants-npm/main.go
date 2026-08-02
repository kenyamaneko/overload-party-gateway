// Command gen-ws-constants-npm は packages/ws-constants の Go 定数から
// npm パッケージが公開する TypeScript 定義を生成する。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kenyamaneko/overload-party-gateway/internal/wsconstgen"
)

func main() {
	input := flag.String("input", "", "WS メッセージ種別の定数を宣言する Go ソースのパス")
	output := flag.String("output", "", "生成する TypeScript ファイルのパス")
	flag.Parse()

	if err := run(*input, *output); err != nil {
		fmt.Fprintf(os.Stderr, "gen-ws-constants-npm: %v\n", err)
		os.Exit(1)
	}
}

func run(inputPath, outputPath string) error {
	if inputPath == "" || outputPath == "" {
		return fmt.Errorf("both -input and -output are required")
	}
	src, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", inputPath, err)
	}
	types, err := wsconstgen.ParseMessageTypes(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, []byte(wsconstgen.RenderTypeScript(types)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}
