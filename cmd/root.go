package cmd

import (
	"github.com/shouni/clibase"
	"github.com/spf13/cobra"

	"github.com/shouni/go-web-reader/internal/config"
)

const (
	appName = "go-web-reader"
)

var opts *config.Config

// addAppPersistentFlags はアプリケーション固有の永続フラグを登録します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().StringVarP(&opts.SourceURL, "uri", "u", "", "入力URL")
}

// initAppPreRunE は設定値を正規化し、必須項目を検証します。
func initAppPreRunE(cmd *cobra.Command, args []string) error {
	opts.Normalize()
	return opts.Validate()
}

// Execute はルートコマンドを構築し、CLI アプリケーションを実行します。
func Execute() {
	opts = &config.Config{}

	clibase.Execute(clibase.App{
		Name:     appName,
		AddFlags: addAppPersistentFlags,
		PreRunE:  initAppPreRunE,
		Commands: []*cobra.Command{
			readCmd,
		},
	})
}
