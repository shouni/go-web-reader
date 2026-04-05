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

// initAppPreRunE は HTTP クライアントの初期化とコンテキストへの格納を行います。
func initAppPreRunE(cmd *cobra.Command, args []string) error {

	return nil
}

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
