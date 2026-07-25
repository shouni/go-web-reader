// Package reader は、HTTP/HTTPS や GCS/S3 など URI の種類を問わず
// コンテンツを読み込み、必要に応じて内容を抽出するユニバーサルリーダーを提供します。
package reader

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
	"github.com/shouni/go-web-exact/v2/extract"
	"github.com/shouni/go-web-exact/v2/ports"
	"github.com/shouni/go-web-reader/internal/closeutil"
	"github.com/shouni/netarmor/securenet"
)

// UniversalReader は URI の種類に応じて読み取りストリームを返します。
type UniversalReader struct {
	mu            sync.Mutex
	extractor     ports.Extractor
	httpClient    HTTPClient
	safeURL       safeURLFunc
	newGCSFactory storageFactoryFunc
	newS3Factory  storageFactoryFunc
	gcs           storageReaderCache
	s3            storageReaderCache
}

// New は UniversalReader の新しいインスタンスを生成します。
func New(opts ...Option) (*UniversalReader, error) {
	cfg := options{
		// securenet.ValidateURL は可変長オプションを取るため、そのままでは
		// safeURLFunc に代入できない。既定ポリシーで呼ぶラッパを噛ませる。
		safeURL: func(ctx context.Context, uri string) error {
			return securenet.ValidateURL(ctx, uri)
		},
		newGCSFactory: func(ctx context.Context) (remoteio.IOFactory, error) { return gcs.New(ctx) },
		newS3Factory:  func(ctx context.Context) (remoteio.IOFactory, error) { return s3.New(ctx) },
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.httpClient == nil {
		cfg.httpClient = httpkit.New(httpkit.DefaultHTTPTimeout)
	}
	if cfg.extractor == nil {
		extractor, err := extract.NewExtractor(httpClientFetcher{client: cfg.httpClient})
		if err != nil {
			return nil, fmt.Errorf("extractorの初期化エラー: %w", err)
		}
		cfg.extractor = extractor
	}
	if cfg.safeURL == nil {
		return nil, fmt.Errorf("safe URL validator is required")
	}
	if cfg.newGCSFactory == nil {
		return nil, fmt.Errorf("GCS factory is required")
	}
	if cfg.newS3Factory == nil {
		return nil, fmt.Errorf("S3 factory is required")
	}

	return &UniversalReader{
		extractor:     cfg.extractor,
		httpClient:    cfg.httpClient,
		safeURL:       cfg.safeURL,
		newGCSFactory: cfg.newGCSFactory,
		newS3Factory:  cfg.newS3Factory,
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

	switch {
	case strings.HasPrefix(uri, securenet.SchemeHTTP), strings.HasPrefix(uri, securenet.SchemeHTTPS):
		return r.openHTTP(ctx, uri)
	case remoteio.IsGCSURI(uri):
		return r.openStorage(ctx, uri, r.getGCSReader)
	case remoteio.IsS3URI(uri):
		return r.openStorage(ctx, uri, r.getS3Reader)
	}

	return nil, fmt.Errorf("適切なリーダーが初期化されていません: %s", uri)
}

// Close は内部で保持している外部リソースを解放します。
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return closeutil.Join(r.gcs.close, r.s3.close)
}
