package reader

import (
	"context"
	"net/http"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
	"github.com/shouni/go-web-exact/v2/ports"
	"github.com/shouni/netarmor/securenet"
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
	fetcher       ports.Fetcher
	httpClient    HTTPClient
	safeURL       SafeURLValidator
	newGCSFactory StorageFactory
	newS3Factory  StorageFactory
}

// newOptions は既定値にオプションを適用した設定を返します。
//
// nil のオプション値は無視します。各 With* が既定値を上書きしない限り、
// 設定が nil になる経路はありません。以前は New 側で「既定値が入っているはずの
// フィールドが nil でないか」を検証していましたが、明示的に nil を渡したときにしか
// 到達しない分岐でした。防ぐべきものを入口で防ぐ形に寄せています。
func newOptions(opts ...Option) options {
	cfg := options{
		// securenet.ValidateURL は可変長オプションを取るため、そのままでは
		// SafeURLValidator に代入できない。既定ポリシーで呼ぶラッパを噛ませる。
		safeURL: func(ctx context.Context, uri string) error {
			return securenet.ValidateURL(ctx, uri)
		},
		httpClient:    httpkit.New(httpkit.DefaultHTTPTimeout),
		newGCSFactory: func(ctx context.Context) (remoteio.IOFactory, error) { return gcs.New(ctx) },
		newS3Factory:  func(ctx context.Context) (remoteio.IOFactory, error) { return s3.New(ctx) },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// Option は UniversalReader の依存を差し替えるためのオプションです。
type Option func(*options)

// WithExtractor はテキスト抽出器を差し替えます。
// HTTP の取得そのものは差し替わりません（そちらは WithFetcher / WithHTTPClient）。
func WithExtractor(extractor ports.Extractor) Option {
	return func(o *options) {
		if extractor != nil {
			o.extractor = extractor
		}
	}
}

// WithFetcher は HTTP(S) の取得処理を差し替えます。
//
// WithHTTPClient がクライアントだけを差し替えるのに対し、こちらは取得処理そのもの
// （リクエスト組み立て・レスポンス処理を含む）を置き換えます。既定の抽出器を使う場合、
// 抽出器にも同じ Fetcher が渡ります。
func WithFetcher(fetcher ports.Fetcher) Option {
	return func(o *options) {
		if fetcher != nil {
			o.fetcher = fetcher
		}
	}
}

// WithHTTPClient は HTTP(S) の取得に使うクライアントを差し替えます。
func WithHTTPClient(client HTTPClient) Option {
	return func(o *options) {
		if client != nil {
			o.httpClient = client
		}
	}
}

// WithSafeURLValidator は URL 安全性検証関数を差し替えます。
func WithSafeURLValidator(fn SafeURLValidator) Option {
	return func(o *options) {
		if fn != nil {
			o.safeURL = fn
		}
	}
}

// WithGCSFactory は GCS ファクトリ生成処理を差し替えます。
func WithGCSFactory(fn StorageFactory) Option {
	return func(o *options) {
		if fn != nil {
			o.newGCSFactory = fn
		}
	}
}

// WithS3Factory は S3 ファクトリ生成処理を差し替えます。
func WithS3Factory(fn StorageFactory) Option {
	return func(o *options) {
		if fn != nil {
			o.newS3Factory = fn
		}
	}
}
