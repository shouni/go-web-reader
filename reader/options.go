package reader

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
	"github.com/shouni/go-web-reader/extract"
	"github.com/shouni/netarmor/securenet"
)

// リトライの既定値です。httpkit の既定（初期 5 秒・最大 30 秒）より短くしています。
// Open は呼び出し側を待たせる同期 API なので、待ち時間の合計が体感を直接左右します。
const (
	// DefaultMaxRetries は HTTP 取得を再試行する既定の回数です（初回の実行を含みません）。
	DefaultMaxRetries = 2
	// DefaultRetryInitialInterval は再試行までの初期待機時間です。
	DefaultRetryInitialInterval = 500 * time.Millisecond
	// DefaultRetryMaxInterval は再試行までの待機時間の上限です。
	DefaultRetryMaxInterval = 4 * time.Second
)

// Extractor は取得済みの HTML から本文テキストを抽出します。
// 第2戻り値は本文が見つかったかどうかです。
type Extractor interface {
	Extract(ctx context.Context, r io.Reader) (text string, hasBody bool, err error)
}

// ContentTypeExtractor は、Content-Type ヘッダーも受け取れる抽出器です。
//
// Extractor とは別の口ではなく、同じ WithExtractor に渡された抽出器が
// 追加で持てる能力です。実装していれば Extract の代わりにこちらが呼ばれ、
// 実装していない抽出器は今までどおり Extract が呼ばれます。
//
// 分けているのは Extractor の互換性のためだけではありません。文字コードの
// 変換は 1 箇所でしか行えず（UTF-8 に直したバイト列をもう一度 Shift_JIS として
// 解釈し直せば壊れます）、その 1 箇所は <meta charset> を読める抽出器側です。
// reader が持つ Content-Type は判定材料として渡すだけに留めます。
type ContentTypeExtractor interface {
	ExtractWithContentType(ctx context.Context, r io.Reader, contentType string) (text string, hasBody bool, err error)
}

// RetryClassifier は、取得の失敗を再試行すべきか判断できる HTTP クライアントです。
//
// HTTPClient が実装していればその判断を優先します。エラーの型を知っているのは
// それを返したクライアント自身なので、既定の httpkit.Client を使う限り
// リトライ対象の定義が reader と httpkit で二重管理になりません。
type RetryClassifier interface {
	IsHTTPRetryableError(err error) bool
}

// SafeURLValidator は URI の安全性を検証します。安全な場合は nil を返します。
// 名前解決を伴うため context を受け取ります。
type SafeURLValidator func(context.Context, string) error

// StorageFactory は GCS/S3 のクライアント一式を生成します。
// 実際の接続確立を伴うため、対象スキームの初回 Open 時にだけ呼ばれます。
type StorageFactory func(context.Context) (remoteio.Factory, error)

// HTTPClient は HTTP リクエストを実行する最小インターフェースです。
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// retryPolicy は HTTP 取得の再試行設定です。
// maxRetries が 0 のときは 1 度だけ実行し、再試行しません。
type retryPolicy struct {
	maxRetries      uint
	initialInterval time.Duration
	maxInterval     time.Duration
}

type options struct {
	extractor     Extractor
	httpClient    HTTPClient
	safeURL       SafeURLValidator
	newGCSFactory StorageFactory
	newS3Factory  StorageFactory
	retry         retryPolicy
}

// newOptions は既定値にオプションを適用した設定を返します。
//
// 各 With* は nil の値を無視するため、設定フィールドが nil になる経路はありません。
// New 側で改めて nil を検査しないのはこのためです。
func newOptions(opts ...Option) options {
	cfg := options{
		// securenet.ValidateURL は可変長オプションを取るため、そのままでは
		// SafeURLValidator に代入できない。既定ポリシーで呼ぶラッパを噛ませる。
		safeURL: func(ctx context.Context, uri string) error {
			return securenet.ValidateURL(ctx, uri)
		},
		extractor:     extract.Engine{},
		httpClient:    httpkit.New(),
		newGCSFactory: func(ctx context.Context) (remoteio.Factory, error) { return gcs.New(ctx) },
		newS3Factory:  func(ctx context.Context) (remoteio.Factory, error) { return s3.New(ctx) },
		retry: retryPolicy{
			maxRetries:      DefaultMaxRetries,
			initialInterval: DefaultRetryInitialInterval,
			maxInterval:     DefaultRetryMaxInterval,
		},
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

// WithMaxRetries は HTTP 取得を再試行する回数を設定します（初回の実行は含みません）。
// 0 を渡すと再試行しません。
//
// 再試行するのは 5xx / 408 / 429 と、分類できない通信エラー（タイムアウトなど）だけです。
// 4xx やレスポンスサイズ超過は、同じリクエストを繰り返しても結果が変わらないため再試行しません。
//
// 既定のクライアントを WithHTTPClient で自前のリトライ付きクライアントに
// 差し替える場合は、二重に待たないよう 0 を渡してください。
func WithMaxRetries(n uint) Option {
	return func(o *options) {
		o.retry.maxRetries = n
	}
}

// WithRetryInterval は再試行までの待機時間（指数バックオフの初期値と上限）を設定します。
// 0 以下の値は無視され、既定値が保たれます。
func WithRetryInterval(initialInterval, maxInterval time.Duration) Option {
	return func(o *options) {
		if initialInterval > 0 {
			o.retry.initialInterval = initialInterval
		}
		if maxInterval > 0 {
			o.retry.maxInterval = maxInterval
		}
	}
}

// WithExtractor はテキスト抽出器を差し替えます。
// HTTP の取得そのものは差し替わりません（そちらは WithHTTPClient）。
//
// 抽出器が ContentTypeExtractor も満たす場合は、Content-Type ヘッダーを添えて
// 呼ばれます（既定の extract.Engine は満たします）。
func WithExtractor(extractor Extractor) Option {
	return func(o *options) {
		if extractor != nil {
			o.extractor = extractor
		}
	}
}

// WithHTTPClient は HTTP(S) の取得に使うクライアントを差し替えます。
//
// リクエストのヘッダーを変えたい場合もここです。Do の中で受け取った
// *http.Request のヘッダーを上書きしてから元のクライアントに委譲できます。
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
