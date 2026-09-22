// Package reader は、HTTP/HTTPS や GCS/S3 など URI の種類を問わず
// コンテンツを読み込み、必要に応じて内容を抽出するユニバーサルリーダーを提供します。
package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"unicode/utf8"

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

// ErrClosed は、Close 済みの UniversalReader を使おうとしたことを表します。
var ErrClosed = errors.New("reader is closed")

// ErrTooLarge は、ReadAllLimit で内容が上限を超えたことを示します。
var ErrTooLarge = errors.New("reader: content exceeds the size limit")

// UniversalReader は URI の種類に応じて読み取りストリームを返します。
type UniversalReader struct {
	extractor  Extractor
	httpClient HTTPClient
	safeURL    SafeURLValidator
	retry      retryPolicy
	// storages はスキーム名をキーにしたストレージリーダーです。スキームを増やす
	// ときに触るのが New のマップ組み立てだけで済むよう、フィールドに直書きしていません。
	storages map[string]*storageReaderCache
	// closed は Close 済みの印です。スキームごとのキャッシュにも同じ印がありますが、
	// 解放するものが無い HTTP には付かないため、それだけだと Close 後も https:// が読めます。
	closed atomic.Bool
}

// Open は URI のスキームを判別し、適切な読み取りストリームを返します。
//
// 扱えるのは https:// / http:// / gs:// / s3:// の 4 つで、それ以外は
// 「未対応のURIスキームです」を返します。ローカルファイルパスは対象外です。
//
// スキームは小文字で書かれている必要があります。判定は "://" の前をそのまま
// 文字列比較するだけで、url.Parse と違って大文字小文字を正規化しないため、
// HTTPS:// のような大文字のスキームは未対応として扱われます。
//
// gs:// / s3:// はストリームを返しますが、HTTP(S) は本文抽出と再試行のために
// レスポンスを読み切ってから返します。返ったストリームを少しずつ読んでも
// メモリは節約になりません（上限は go-http-kit のレスポンスサイズ上限です）。
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

	// スキームの取り出しは go-remote-io と同じ関数です。gs/s3 は URI をそのまま
	// remoteio に渡すので、「どこからがスキームか」の解釈がずれると実バグになります。
	scheme := remoteio.Scheme(uri)

	// 振り分けは URL 安全性検証より先で、検証は HTTP(S) にだけ掛けます。検証は
	// 「自分でダイヤルする相手が安全か」を見るもので、接続先をクラウド SDK が決める
	// gs:// / s3:// には相手がいません。しかも securenet.ValidateURL は http/https 以外を
	// スキーム違反として拒否するので、全スキームに通すと GCS/S3 が一切開けません。
	//
	// 振り分けの条件に securenet の判定（IsSecureServiceURL / ValidateURL）を使わないこと。
	// 前者は平文 HTTP を localhost 等にしか許さず http:// が丸ごと未対応になり、後者は
	// 利用者が差し替えた検証器がどのバックエンドで処理するかまで左右してしまいます。
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
// Open したストリームを読み切って閉じるまでを畳んだもので、閉じ忘れを防ぎます。
func (r *UniversalReader) ReadAll(ctx context.Context, uri string) ([]byte, error) {
	stream, err := r.Open(ctx, uri)
	if err != nil {
		return nil, err
	}

	// 読み取りとクローズ、どちらの失敗も落とさない。
	data, readErr := io.ReadAll(stream)
	if err := errors.Join(readErr, stream.Close()); err != nil {
		return nil, fmt.Errorf("URIの読み込みに失敗しました (%s): %w", uri, err)
	}

	return data, nil
}

// ReadAllLimit は ReadAll と同じですが、内容が maxBytes を超えていれば ErrTooLarge を返します。
//
// ReadAll には上限がありません。HTTP は HandleResponse の 25MB で止まりますが、
// gs:// と s3:// はストリームをそのまま渡すので、上限を掛けるのは呼び出し側の仕事に
// なっていました。同じ「上限より 1 バイト多く読んで超過を見分ける」手順を各サービスが
// 書いていたので、ここに置きます。切り捨てて使いたい場合は ReadText を使ってください。
func (r *UniversalReader) ReadAllLimit(ctx context.Context, uri string, maxBytes int64) ([]byte, error) {
	data, truncated, err := r.readLimited(ctx, uri, maxBytes)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, fmt.Errorf("%w: %s (limit: %d bytes)", ErrTooLarge, uri, maxBytes)
	}
	return data, nil
}

// ReadText は URI の内容を文字列として最大 maxBytes まで読み、上限を超えていれば
// UTF-8 の文字境界で切り捨てて返します。第 2 戻り値は切り捨てたかどうかです。
//
// プロンプトに載せる本文のように「長すぎる分は落としてよい」用途のための口で、
// 切り捨てたことは呼び出し側が記録・通知してください。ここでは黙って切ります。
func (r *UniversalReader) ReadText(ctx context.Context, uri string, maxBytes int64) (string, bool, error) {
	data, truncated, err := r.readLimited(ctx, uri, maxBytes)
	if err != nil {
		return "", false, err
	}
	if truncated {
		// 上限で切った末尾は多バイト文字の途中でありうる。不完全なバイトを落とす。
		for len(data) > 0 {
			if r, size := utf8.DecodeLastRune(data); r == utf8.RuneError && size == 1 {
				data = data[:len(data)-1]
				continue
			}
			break
		}
	}
	return string(data), truncated, nil
}

// readLimited は、上限より 1 バイト多く読んで、超過したかどうかを見分けます。
func (r *UniversalReader) readLimited(ctx context.Context, uri string, maxBytes int64) (data []byte, truncated bool, err error) {
	if maxBytes <= 0 {
		return nil, false, fmt.Errorf("maxBytes must be positive: %d", maxBytes)
	}
	stream, err := r.Open(ctx, uri)
	if err != nil {
		return nil, false, err
	}

	data, readErr := io.ReadAll(io.LimitReader(stream, maxBytes+1))
	if err := errors.Join(readErr, stream.Close()); err != nil {
		return nil, false, fmt.Errorf("URIの読み込みに失敗しました (%s): %w", uri, err)
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

// Close は内部で保持している外部リソースを解放します。
// Close は終端です。スキームを問わず、解放後の Open は ErrClosed を返します。
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}
	r.closed.Store(true)

	// 最初の失敗で打ち切らず全部閉じる。閉じ損ねた側の接続が残るため。
	var errs []error
	for _, cache := range r.storages {
		if err := cache.close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
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
