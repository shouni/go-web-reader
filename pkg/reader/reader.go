// Package reader は、HTTP/HTTPS や GCS/S3 など URI の種類を問わず
// コンテンツを読み込み、必要に応じて内容を抽出するユニバーサルリーダーを提供します。
package reader

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-web-exact/v2/extract"
	"github.com/shouni/go-web-exact/v2/ports"
	"github.com/shouni/go-web-reader/internal/closeutil"
	"github.com/shouni/netarmor/securenet"
)

// UniversalReader は URI の種類に応じて読み取りストリームを返します。
type UniversalReader struct {
	extractor ports.Extractor
	fetcher   ports.Fetcher
	safeURL   SafeURLValidator
	// storages はスキームのプレフィックスをキーにしたストレージリーダーです。
	// スキームを増やすときに触るのがこのマップの組み立てだけで済むよう、
	// フィールドに直書きしていません（以前は追加のたびに構造体・New・Open・Close の
	// 4 箇所を触る必要がありました）。
	storages map[string]*storageReaderCache
}

// New は UniversalReader の新しいインスタンスを生成します。
func New(opts ...Option) (*UniversalReader, error) {
	cfg := newOptions(opts...)

	fetcher := cfg.fetcher
	if fetcher == nil {
		fetcher = httpClientFetcher{client: cfg.httpClient}
	}

	extractor := cfg.extractor
	if extractor == nil {
		built, err := extract.NewExtractor(fetcher)
		if err != nil {
			return nil, fmt.Errorf("extractorの初期化エラー: %w", err)
		}
		extractor = built
	}

	return &UniversalReader{
		extractor: extractor,
		fetcher:   fetcher,
		safeURL:   cfg.safeURL,
		storages: map[string]*storageReaderCache{
			remoteio.PrefixGCS: {label: "GCS", newFactory: cfg.newGCSFactory},
			remoteio.PrefixS3:  {label: "S3", newFactory: cfg.newS3Factory},
		},
	}, nil
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

	if strings.HasPrefix(uri, securenet.SchemeHTTP) || strings.HasPrefix(uri, securenet.SchemeHTTPS) {
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
// スキームごとに独立してロックするため、片方の解放がもう片方を待たせることはありません。
// Close は終端です。解放後の Open は ErrClosed を返します。
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}

	fns := make([]func() error, 0, len(r.storages))
	for _, cache := range r.storages {
		fns = append(fns, cache.close)
	}

	return closeutil.Join(fns...)
}
