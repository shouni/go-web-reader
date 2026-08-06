// Package cmd は、go-web-reader CLI のサブコマンドとその実行ロジックを定義します。
package cmd

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/shouni/go-web-reader/internal/builder"
)

// readCmd は 'read' サブコマンドを定義します。
var readCmd = &cobra.Command{
	Use:   "read",
	Short: "指定されたURIからコンテンツを読み込み、標準出力に出力します。",
	Long:  `このコマンドは、指定されたURIからコンテキストを取得し、その結果を標準出力に直接表示します。`,
	Args:  cobra.NoArgs,
	RunE:  readCommand,
}

// --------------------------------------------------------------------------
// コマンドの実行ロジック
// --------------------------------------------------------------------------

// readCommand は、指定されたURIからコンテキストを取得し、その結果を標準出力に直接表示します。
//
// 標準出力へは取得内容だけを流し、見出しや区切り線は標準エラーへ出します。
// 装飾を混ぜると `go-web-reader read -u ... > out.txt` や他コマンドへのパイプで
// そのまま使えなくなるためです。
func readCommand(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	appCtx, err := builder.BuildContainer(opts)
	if err != nil {
		return fmt.Errorf("アプリケーションコンテキストの構築に失敗しました: %w", err)
	}
	defer func() {
		slog.Info("♻️ アプリケーションコンテキストをクローズ中...")
		if err := appCtx.Close(); err != nil {
			slog.Warn("アプリケーションコンテキストのクローズに失敗しました。", "error", err)
		}
	}()

	out := &decoratedWriter{
		out:    cmd.OutOrStdout(),
		notice: cmd.ErrOrStderr(),
		header: "--- 取得結果 ---",
	}
	if err := appCtx.Pipeline.Execute(ctx, out); err != nil {
		return fmt.Errorf("実行に失敗しました: %w", err)
	}
	out.finish("-----------------------------------------------------")

	slog.Info("結果を標準出力に出力しました。")

	return nil
}

// decoratedWriter は、最初の 1 バイトが書かれたときにだけ見出しを notice へ出します。
//
// 取得前に見出しを出すと、取得に失敗したときにも「--- 取得結果 ---」だけが
// 残ります。出力があったときにだけ枠を出すため、書き込みまで遅延させます。
type decoratedWriter struct {
	out     io.Writer
	notice  io.Writer
	header  string
	started bool
}

// Write は最初の呼び出しで見出しを出してから、本文を out へ流します。
func (w *decoratedWriter) Write(p []byte) (int, error) {
	if !w.started && len(p) > 0 {
		w.started = true
		_, _ = fmt.Fprintln(w.notice, w.header)
	}
	return w.out.Write(p)
}

// finish は、本文が 1 バイトでも出ていれば閉じの区切り線を notice へ出します。
func (w *decoratedWriter) finish(footer string) {
	if !w.started {
		return
	}
	_, _ = fmt.Fprintln(w.notice, "\n"+footer)
}
