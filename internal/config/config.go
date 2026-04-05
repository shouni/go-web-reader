package config

import (
	"strings"
	"time"

	"github.com/shouni/go-utils/envutil"
)

const (
	DefaultHTTPTimeout = 30 * time.Second
)

// Config はAIコードレビューに必要なすべての設定を含みます。
// この構造体は、コマンドライン引数からサービスロジックへ設定を渡すための共通のデータモデルです。
type Config struct {
	GCSBucket string
}

// Normalize は設定値の文字列フィールドから前後の空白を一括で削除します。
func (c *Config) Normalize() {
	if c == nil {
		return
	}
	c.GCSBucket = strings.TrimSpace(c.GCSBucket)
}

// LoadConfig は環境変数から設定を読み込みます。
func LoadConfig() *Config {
	return &Config{
		GCSBucket: envutil.GetEnv("GCS_BUCKET", ""),
	}
}
