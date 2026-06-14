package reader

import (
	"context"
	"errors"
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
	"github.com/shouni/netarmor/securenet"
)

// UniversalReader はあらゆるURIからデータを読み取るインターフェース
type UniversalReader struct {
	mu            sync.Mutex
	extractor     ports.Extractor
	safeURL       safeURLFunc
	newGCSFactory storageFactoryFunc
	newS3Factory  storageFactoryFunc
	gcs           storageReaderCache
	s3            storageReaderCache
}

type storageReaderCache struct {
	reader remoteio.Reader
	closer io.Closer
}

// New はUniversalReaderの新しいインスタンスを生成します。
func New(opts ...Option) (*UniversalReader, error) {
	cfg := options{
		safeURL:       securenet.IsSafeURL,
		newGCSFactory: func(ctx context.Context) (remoteio.IOFactory, error) { return gcs.New(ctx) },
		newS3Factory:  func(ctx context.Context) (remoteio.IOFactory, error) { return s3.New(ctx) },
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.extractor == nil {
		httpClient := httpkit.New(httpkit.DefaultHTTPTimeout)
		extractor, err := extract.NewExtractor(httpClient)
		if err != nil {
			return nil, fmt.Errorf("Extractorの初期化エラー: %w", err)
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
		safeURL:       cfg.safeURL,
		newGCSFactory: cfg.newGCSFactory,
		newS3Factory:  cfg.newS3Factory,
	}, nil
}

// Open は URI のスキームを判別し、適切なリーダーを返します
func (r *UniversalReader) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if uri == "" {
		return nil, fmt.Errorf("uri cannot be empty")
	}
	ok, err := r.safeURL(uri)
	if err != nil {
		return nil, fmt.Errorf("URL安全性検証に失敗しました: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("安全ではないURLです: %s", uri)
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

// openHTTP は HTTP(S) URI から本文テキストを抽出し、読み取りストリームとして返します。
func (r *UniversalReader) openHTTP(ctx context.Context, uri string) (io.ReadCloser, error) {
	text, hasBody, err := r.extractor.FetchAndExtractText(ctx, uri)
	if err != nil {
		return nil, err
	}
	if !hasBody {
		return nil, fmt.Errorf("コンテンツが見つかりませんでした: %s", uri)
	}

	return io.NopCloser(strings.NewReader(text)), nil
}

// openStorage は指定されたストレージリーダーを取得し、URI の読み取りストリームを返します。
func (r *UniversalReader) openStorage(
	ctx context.Context,
	uri string,
	getReader func(context.Context) (remoteio.Reader, error),
) (io.ReadCloser, error) {
	reader, err := getReader(ctx)
	if err != nil {
		return nil, err
	}

	return reader.Open(ctx, uri)
}

// Close は内部で保持している外部リソースを解放します。
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, cache := range []*storageReaderCache{&r.gcs, &r.s3} {
		if err := cache.close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// getGCSReader は、ストレージリーダーの生成とクロージャの管理
func (r *UniversalReader) getGCSReader(ctx context.Context) (remoteio.Reader, error) {
	return r.getStorageReader(ctx, &r.gcs, r.newGCSFactory, "GCS")
}

// getS3Reader は、S3リーダーの取得と管理
func (r *UniversalReader) getS3Reader(ctx context.Context) (remoteio.Reader, error) {
	return r.getStorageReader(ctx, &r.s3, r.newS3Factory, "S3")
}

// getStorageReader はストレージリーダーを遅延初期化し、以後の呼び出しで再利用します。
func (r *UniversalReader) getStorageReader(
	ctx context.Context,
	cache *storageReaderCache,
	newFactory storageFactoryFunc,
	label string,
) (remoteio.Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cache.reader != nil {
		return cache.reader, nil
	}

	reader, closer, err := newStorageReader(ctx, newFactory)
	if err != nil {
		return nil, fmt.Errorf("%sリーダーの生成に失敗: %w", label, err)
	}
	cache.reader = reader
	cache.closer = closer

	return cache.reader, nil
}

// newStorageReader は、ストレージリーダーの生成とクロージャの管理
func newStorageReader(
	ctx context.Context,
	newFactory func(context.Context) (remoteio.IOFactory, error),
) (remoteio.Reader, io.Closer, error) {
	factory, err := newFactory(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("ストレージファクトリの生成に失敗: %w", err)
	}

	reader, err := factory.InputReader()
	if err != nil {
		_ = factory.Close()
		return nil, nil, fmt.Errorf("リーダーの生成に失敗: %w", err)
	}

	if reader == nil {
		_ = factory.Close()
		return nil, nil, fmt.Errorf("リーダーの生成に失敗: reader is nil")
	}

	return reader, factory, nil
}

// close は保持しているクローザーを閉じ、キャッシュ済みリーダーを解放します。
func (c *storageReaderCache) close() error {
	if c.closer == nil {
		c.reader = nil
		return nil
	}

	err := c.closer.Close()
	c.reader = nil
	c.closer = nil

	return err
}
