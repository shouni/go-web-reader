package reader

import (
	"context"
	"net/http"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-web-exact/v2/ports"
)

type safeURLFunc func(string) (bool, error)
type storageFactoryFunc func(context.Context) (remoteio.IOFactory, error)

// HTTPClient は HTTP リクエストを実行する最小インターフェースです。
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type options struct {
	extractor     ports.Extractor
	httpClient    HTTPClient
	safeURL       safeURLFunc
	newGCSFactory storageFactoryFunc
	newS3Factory  storageFactoryFunc
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
func WithSafeURLValidator(fn safeURLFunc) Option {
	return func(o *options) {
		o.safeURL = fn
	}
}

// WithGCSFactory は GCS ファクトリ生成処理を差し替えます。
func WithGCSFactory(fn storageFactoryFunc) Option {
	return func(o *options) {
		o.newGCSFactory = fn
	}
}

// WithS3Factory は S3 ファクトリ生成処理を差し替えます。
func WithS3Factory(fn storageFactoryFunc) Option {
	return func(o *options) {
		o.newS3Factory = fn
	}
}
