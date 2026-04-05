package cmd

import (
	"github.com/shouni/clibase"
	"github.com/spf13/cobra"
)

const (
	appName           = "go-web-reader"
	defaultTimeoutSec = 10
)

type AppFlags struct {
	URI string
}

var appFlags AppFlags

// --- アプリケーション固有のカスタム関数 ---

// addAppPersistentFlags はアプリケーション固有の永続フラグを登録します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().StringVarP(&appFlags.URI, "uri", "u", "", "入力URL")
}

// initAppPreRunE は HTTP クライアントの初期化とコンテキストへの格納を行います。
func initAppPreRunE(cmd *cobra.Command, args []string) error {

	return nil
}

// --- エントリポイント ---

func Execute() {
	clibase.Execute(clibase.App{
		Name:     appName,
		AddFlags: addAppPersistentFlags,
		PreRunE:  initAppPreRunE,
		Commands: []*cobra.Command{},
	})
}
