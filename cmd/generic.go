package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/shouni/go-web-reader/internal/builder"
	"github.com/shouni/go-web-reader/internal/config"
)

// opts は、レビュー実行のパラメータです
var opts config.Config

// genericCmd は 'generic' サブコマンドを定義します。
var genericCmd = &cobra.Command{
	Use:   "generic",
	Short: "コードレビューを実行し、その結果を標準出力に出力します。",
	Long:  `このコマンドは、指定されたGitリポジトリのブランチ間の差分をAIでレビューし、その結果を標準出力に直接表示します。外部サービスとの連携は行いません。`,
	Args:  cobra.NoArgs,
	RunE:  genericCommand,
}

// --------------------------------------------------------------------------
// コマンドの実行ロジック
// --------------------------------------------------------------------------

// genericCommand は、リモートリポジトリのブランチ比較を Gemini AI に依頼し、
// 結果を標準出力に出力する generic コマンドの実行ロジックです。
func genericCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	appCtx, err := builder.BuildContainer(ctx, &opts)
	if err != nil {
		return fmt.Errorf("アプリケーションコンテキストの構築に失敗しました: %w", err)
	}
	defer func() {
		slog.Info("♻️ アプリケーションコンテキストをクローズ中...")
		appCtx.Close()
	}()

	// 結果の出力
	// TODO: 実際のAIレビュー処理を実装し、その結果を渡す
	// reviewResult, err := runAIReview(appCtx)
	// if err != nil { ... }
	printReviewResult("test") // FIXME: reviewResult に置き換える
	slog.Info("レビュー結果を標準出力に出力しました。")

	return nil
}

// printReviewResult はレビュー結果を標準出力にフォーマットして表示します。
func printReviewResult(result string) {
	fmt.Println("\n--- 取得結果 ---")
	fmt.Println(result)
	fmt.Println("-----------------------------------------------------")
}
