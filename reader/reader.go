// Package reader は、HTTP/HTTPS や GCS/S3 など URI の種類を問わず
// コンテンツを読み込み、必要に応じて内容を抽出するユニバーサルリーダーを提供します。
package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/netarmor/securenet"
)

// HTTP(S) のスキーム名です。remoteio.SchemeGCS / SchemeS3 と同じ粒度に揃えてあり、
// Open の振り分けはすべてこの形の文字列比較で済みます。
//
// 綴りは netarmor の定数から取ります。securenet が「これは http だ」と見なす綴りと、
// こちらが HTTP として振り分ける綴りがずれると、検証を通り抜けた URI が別の枝に落ちます。
const (
	schemeHTTP  = securenet.SchemeHTTP
	schemeHTTPS = securenet.SchemeHTTPS
)

// UniversalReader は URI の種類に応じて読み取りストリームを返します。
type UniversalReader struct {
	extractor  Extractor
	httpClient HTTPClient
	safeURL    SafeURLValidator
	retry      retryPolicy
	// storages はスキームのプレフィックスをキーにしたストレージリーダーです。
	// スキームを増やすときに触るのがこのマップの組み立てだけで済むよう、
	// フィールドに直書きしていません（以前は追加のたびに構造体・New・Open・Close の
	// 4 箇所を触る必要がありました）。
	storages map[string]*storageReaderCache
	// closed は Close 済みかどうかです。スキームごとのキャッシュにも同じ印は
	// ありますが、それだけだと HTTP には解放するものが無いぶん印が付かず、
	// Close 後も https:// だけ読めてしまいます。
	closed atomic.Bool
}

// New は UniversalReader の新しいインスタンスを生成します。
//
// エラーを返さないのは、ここで確立する外部接続がないためです。GCS/S3 の
// クライアントは対象スキームの初回 Open まで作られず、失敗するとしたらそちらです。
func New(opts ...Option) *UniversalReader {
	cfg := newOptions(opts...)

	return &UniversalReader{
		extractor:  cfg.extractor,
		httpClient: cfg.httpClient,
		safeURL:    cfg.safeURL,
		retry:      cfg.retry,
		storages: map[string]*storageReaderCache{
			remoteio.SchemeGCS: {label: "GCS", newFactory: cfg.newGCSFactory},
			remoteio.SchemeS3:  {label: "S3", newFactory: cfg.newS3Factory},
		},
	}
}

// Open は URI のスキームを判別し、適切な読み取りストリームを返します。
func (r *UniversalReader) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if uri == "" {
		return nil, fmt.Errorf("uri cannot be empty")
	}
	if r.closed.Load() {
		return nil, fmt.Errorf("リーダーは利用できません: %w", ErrClosed)
	}

	// スキームの取り出しは go-remote-io と同じ関数を使います。自前で判定を書くと、
	// 「どこからがスキームか」の解釈が両者でずれます。HTTP(S) も同じ関数を通すことで、
	// 振り分けの入口がスキームによらず 1 つになります。
	scheme := remoteio.Scheme(uri)

	// 振り分けは URL 安全性検証より先です。検証は「自分でダイヤルする相手が安全か」を
	// 見るものなので、接続先をクラウド SDK が決める gs:// / s3:// には意味がありません。
	// それどころか securenet.ValidateURL は http/https 以外をスキーム違反として拒否する
	// ため、全スキームに通すと GCS/S3 が一切開けません。
	//
	// なお securenet.IsSecureServiceURL はここでは使えません。あれは「安全なスキームか」
	// を見る方針判定で、平文 HTTP は localhost 等にしか true を返しません。振り分けに使うと
	// http:// が丸ごと未対応スキーム扱いになり、通るのは直後の検証で弾かれる URL だけになります。
	if scheme == schemeHTTP || scheme == schemeHTTPS {
		if err := r.safeURL(ctx, uri); err != nil {
			return nil, fmt.Errorf("URL安全性検証に失敗しました: %w", err)
		}
		return r.openHTTP(ctx, uri)
	}
	if cache, ok := r.storages[scheme]; ok {
		return r.openStorage(ctx, uri, cache)
	}

	return nil, fmt.Errorf("未対応のURIスキームです: %s", uri)
}

// ReadAll は URI の内容を最後まで読み込んで返します。
//
// Open で得たストリームを読み切って閉じるだけの定型を畳んだものです。
// このライブラリの用途では読み切ってから扱うことがほとんどで、
// 呼び出しごとに Close を書かせると閉じ忘れがそのままリークになります。
func (r *UniversalReader) ReadAll(ctx context.Context, uri string) ([]byte, error) {
	stream, err := r.Open(ctx, uri)
	if err != nil {
		return nil, err
	}

	// 読み取りの失敗とクローズの失敗はどちらも握り潰しません。
	data, readErr := io.ReadAll(stream)
	if err := errors.Join(readErr, stream.Close()); err != nil {
		return nil, fmt.Errorf("URIの読み込みに失敗しました (%s): %w", uri, err)
	}

	return data, nil
}

// Close は内部で保持している外部リソースを解放します。
// Close は終端です。スキームを問わず、解放後の Open は ErrClosed を返します。
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}
	r.closed.Store(true)

	// 最初の失敗で打ち切らず全部閉じます。片方の解放失敗で
	// もう片方を閉じ損ねると、そちらの接続が残ります。
	var errs []error
	for _, cache := range r.storages {
		if err := cache.close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
