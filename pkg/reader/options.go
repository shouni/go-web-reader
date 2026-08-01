package reader

import (
	"context"
	"net/http"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-web-exact/v2/ports"
)

// SafeURLValidator は URI の安全性を検証します。安全な場合は nil を返します。
// 名前解決を伴うため context を受け取ります。
type SafeURLValidator func(context.Context, string) error

// StorageFactory は GCS/S3 のクライアント一式を生成します。
// 実際の接続確立を伴うため、対象スキームの初回 Open 時にだけ呼ばれます。
type StorageFactory func(context.Context) (remoteio.IOFactory, error)

// HTTPClient は HTTP リクエストを実行する最小インターフェースです。
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type options struct {
	extractor     ports.Extractor
	httpClient    HTTPClient
	safeURL       SafeURLValidator
	newGCSFactory StorageFactory
	newS3Factory  StorageFactory
}

// Option は UniversalReader の依存を差し替えるためのオプションです。
type Option func(*options)

// WithExtractor はテキスト抽出器を差し替えます。
func WithExtractor(extractor ports.Extractor) Option {
	return func(o *options) {
		o.extractor = extractor
	}
}

// WithHTTPClient は HTTP(S) の取得に使うクライアントを差し替えます。
func WithHTTPClient(client HTTPClient) Option {
	return func(o *options) {
		o.httpClient = client
	}
}

// WithSafeURLValidator は URL 安全性検証関数を差し替えます。
func WithSafeURLValidator(fn SafeURLValidator) Option {
	return func(o *options) {
		o.safeURL = fn
	}
}

// WithGCSFactory は GCS ファクトリ生成処理を差し替えます。
func WithGCSFactory(fn StorageFactory) Option {
	return func(o *options) {
		o.newGCSFactory = fn
	}
}

// WithS3Factory は S3 ファクトリ生成処理を差し替えます。
func WithS3Factory(fn StorageFactory) Option {
	return func(o *options) {
		o.newS3Factory = fn
	}
}
