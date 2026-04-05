package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/shouni/go-web-reader/internal/builder"
)

// readCmd は 'fetch' サブコマンドを定義します。
var readCmd = &cobra.Command{
	Use:   "read",
	Short: "コードレビューを実行し、その結果を標準出力に出力します。",
	Long:  `このコマンドは、指定されたURIからコンテキストを取得し、その結果を標準出力に直接表示します。`,
	Args:  cobra.NoArgs,
	RunE:  readCommand,
}

// --------------------------------------------------------------------------
// コマンドの実行ロジック
// --------------------------------------------------------------------------

// readCommand は、指定されたURIからコンテキストを取得し、その結果を標準出力に直接表示します。
func readCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	appCtx, err := builder.BuildContainer(ctx, opts)
	if err != nil {
		return fmt.Errorf("アプリケーションコンテキストの構築に失敗しました: %w", err)
	}
	defer func() {
		slog.Info("♻️ アプリケーションコンテキストをクローズ中...")
		appCtx.Close()
	}()

	result, err := appCtx.Pipeline.Execute(ctx)
	if err != nil {
		return fmt.Errorf("実行に失敗しました: %w", err)
	}
	printResult(result)
	slog.Info("結果を標準出力に出力しました。")

	return nil
}

// printResult は結果を標準出力にフォーマットして表示します。
func printResult(result string) {
	fmt.Println("\n--- 取得結果 ---")
	fmt.Println(result)
	fmt.Println("-----------------------------------------------------")
}
