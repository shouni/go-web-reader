// Package reader は、HTTP/HTTPS や GCS/S3 など URI の種類を問わず
// コンテンツを読み込み、必要に応じて内容を抽出するユニバーサルリーダーを提供します。
package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/netarmor/securenet"
)

// UniversalReader は URI の種類に応じて読み取りストリームを返します。
type UniversalReader struct {
	extractor  Extractor
	httpClient HTTPClient
	safeURL    SafeURLValidator
	// storages はスキームのプレフィックスをキーにしたストレージリーダーです。
	// スキームを増やすときに触るのがこのマップの組み立てだけで済むよう、
	// フィールドに直書きしていません（以前は追加のたびに構造体・New・Open・Close の
	// 4 箇所を触る必要がありました）。
	storages map[string]*storageReaderCache
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
		storages: map[string]*storageReaderCache{
			remoteio.PrefixGCS: {label: "GCS", newFactory: cfg.newGCSFactory},
			remoteio.PrefixS3:  {label: "S3", newFactory: cfg.newS3Factory},
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
	if err := r.safeURL(ctx, uri); err != nil {
		return nil, fmt.Errorf("URL安全性検証に失敗しました: %w", err)
	}

	// securenet の定数はスキーム名だけ（"http" / "https"）なので "://" まで見ます。
	// スキーム名だけで前方一致すると "https://" は "http" にも一致して第 2 条件が
	// 死ぬうえ、"httpfoo://" のような別スキームまで HTTP 扱いになります。
	if strings.HasPrefix(uri, securenet.SchemeHTTP+"://") || strings.HasPrefix(uri, securenet.SchemeHTTPS+"://") {
		return r.openHTTP(ctx, uri)
	}
	// スキームの取り出しは go-remote-io と同じ関数を使います。自前で判定を書くと、
	// 「どこからがスキームか」の解釈が両者でずれます。
	if cache, ok := r.storages[remoteio.SchemePrefix(uri)]; ok {
		return r.openStorage(ctx, uri, cache)
	}

	return nil, fmt.Errorf("未対応のURIスキームです: %s", uri)
}

// Close は内部で保持している外部リソースを解放します。
// Close は終端です。解放後の Open は ErrClosed を返します。
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}

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
