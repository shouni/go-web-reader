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
// 別の口ではなく、WithExtractor に渡した抽出器が追加で持てる能力です。
// 満たしていれば Extract の代わりにこちらが呼ばれます。
//
// 文字コードの変換は <meta charset> を読める抽出器側の 1 箇所でしか行えないため
// （UTF-8 に直したバイト列を再度 Shift_JIS と解釈すれば壊れます）、
// reader は Content-Type を判定材料として渡すだけで変換はしません。
type ContentTypeExtractor interface {
	ExtractWithContentType(ctx context.Context, r io.Reader, contentType string) (text string, hasBody bool, err error)
}

// HTTPClient は HTTP リクエストを実行する最小インターフェースです。
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// RetryClassifier は、取得の失敗を再試行すべきか判断できる HTTP クライアントです。
// HTTPClient が満たしていればその判断を優先します。エラーの型を知っているのは
// それを返したクライアントなので、リトライ対象の定義を reader 側で二重管理しません。
type RetryClassifier interface {
	IsHTTPRetryableError(err error) bool
}

// SafeURLValidator は URI の安全性を検証します。安全な場合は nil を返します。
// 名前解決を伴うため context を受け取ります。
type SafeURLValidator func(context.Context, string) error

// StorageFactory は GCS/S3 のクライアント一式を生成します。
// 実際の接続確立を伴うため、対象スキームの初回 Open 時にだけ呼ばれます。
type StorageFactory func(context.Context) (remoteio.Factory, error)

// Option は UniversalReader の依存を差し替えるためのオプションです。
//
// nil の Option、および nil の値を渡した With* は無視され、既定値が保たれます。
// 差し替えたつもりで既定のまま動くので、渡す値が nil でないことは呼び出し側で確かめてください。
type Option func(*options)

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
// 各 With* が nil を無視するため、フィールドが nil になる経路はなく、New は再検査しません。
func newOptions(opts ...Option) options {
	cfg := options{
		// securenet.ValidateURL は可変長オプションを取るため、そのままでは代入できない。
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

// WithMaxRetries は HTTP 取得を再試行する回数を設定します（初回の実行は含みません）。
// 0 を渡すと再試行しません。
//
// 再試行するのは 5xx / 408 / 429 と、分類できない通信エラー（タイムアウトなど）だけです。
// 4xx やレスポンスサイズ超過は、同じリクエストを繰り返しても結果が変わらないため再試行しません。
//
// レスポンスに Retry-After があった場合、次の待機時間は指数バックオフの算出値ではなく
// その指示値になります。サーバーが待てと言った時間より早く送り直しても、同じ拒否が
// 返るだけだからです。ただし指示値が WithRetryInterval の上限を超える場合は、待たずに
// 打ち切って retry.ErrRetryAfterTooLong を返します。叩く先は利用者が入力した URL で、
// 上限が無いと相手に待ち時間を決めさせることになります。
//
// 既定のクライアントを WithHTTPClient で自前のリトライ付きクライアントに
// 差し替える場合は、二重に待たないよう 0 を渡してください。
func WithMaxRetries(n uint) Option {
	return func(o *options) {
		o.retry.maxRetries = n
	}
}

// WithRetryInterval は再試行までの待機時間（指数バックオフの初期値と上限）を設定します。
// 0 以下の値は無視され、既定値が保たれます。上限は Retry-After の指示にも掛かります
// （WithMaxRetries を参照）。
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
// リクエストヘッダーを変えたい場合もここです。Do の中で *http.Request を
// 書き換えてから元のクライアントに委譲してください。
//
// レスポンスサイズの上限は外れません。上限はクライアントの外側（返ってきた
// *http.Response を読み切る go-http-kit の処理）で掛かるためです。
func WithHTTPClient(client HTTPClient) Option {
	return func(o *options) {
		if client != nil {
			o.httpClient = client
		}
	}
}

// WithSafeURLValidator は URL 安全性検証関数を差し替えます。
//
// 検証器が呼ばれるのは HTTP(S) の枝だけです。gs:// / s3:// は接続先をクラウド SDK が
// 決めるため検証を通らず、ストレージ側の URI を弾く口ではありません。
//
// ローカルのテストサーバーへ向けるなら WithHTTPClient も差し替えてください。
// 検証器だけ緩めても、既定のクライアントが接続直前に行う IP 検証で落ちます。
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
