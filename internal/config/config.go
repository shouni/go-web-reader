package config

import (
	"fmt"
	"strings"
)

// Config は、コマンドライン引数からサービスロジックへ設定を渡すための共通のデータモデルです。
type Config struct {
	SourceURL string
}

// Normalize は設定値の文字列フィールドから前後の空白を一括で削除します。
func (c *Config) Normalize() {
	if c == nil {
		return
	}
	c.SourceURL = strings.TrimSpace(c.SourceURL)
}

// Validate は必須設定の整合性を検証します。
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.SourceURL == "" {
		return fmt.Errorf("source_url is required")
	}

	return nil
}
