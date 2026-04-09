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
	mu        sync.Mutex
	extractor ports.Extractor
	gcsReader remoteio.Reader
	gcsCloser io.Closer
	s3Reader  remoteio.Reader
	s3Closer  io.Closer
}

// New はUniversalReaderの新しいインスタンスを生成します。
func New() (*UniversalReader, error) {
	httpClient := httpkit.New(httpkit.DefaultHTTPTimeout)
	extractor, err := extract.NewExtractor(httpClient)
	if err != nil {
		return nil, fmt.Errorf("Extractorの初期化エラー: %w", err)
	}

	return &UniversalReader{
		extractor: extractor,
	}, nil
}

// Read は URI のスキームを判別し、適切なリーダーを返します
func (r *UniversalReader) Read(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if uri == "" {
		return nil, fmt.Errorf("uri cannot be empty")
	}
	ok, err := securenet.IsSafeURL(uri)
	if err != nil || !ok {
		return nil, fmt.Errorf("安全ではないURLです: %s", uri)
	}

	switch {
	case strings.HasPrefix(uri, securenet.SchemeHTTP), strings.HasPrefix(uri, securenet.SchemeHTTPS):
		text, hasBody, err := r.extractor.FetchAndExtractText(ctx, uri)
		if err != nil {
			return nil, err
		}
		if !hasBody {
			return nil, fmt.Errorf("コンテンツが見つかりませんでした: %s", uri)
		}
		return io.NopCloser(strings.NewReader(text)), nil
	case remoteio.IsGCSURI(uri):
		reader, err := r.getGCSReader(ctx)
		if err != nil {
			return nil, err
		}
		return reader.Open(ctx, uri)
	case remoteio.IsS3URI(uri):
		reader, err := r.getS3Reader(ctx)
		if err != nil {
			return nil, err
		}
		return reader.Open(ctx, uri)
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

	var errs []error
	if r.gcsCloser != nil {
		errs = append(errs, r.gcsCloser.Close())
		r.gcsCloser = nil
		r.gcsReader = nil
	}
	if r.s3Closer != nil {
		errs = append(errs, r.s3Closer.Close())
		r.s3Closer = nil
		r.s3Reader = nil
	}

	return errors.Join(errs...)
}

// getGCSReader は、ストレージリーダーの生成とクロージャの管理
func (r *UniversalReader) getGCSReader(ctx context.Context) (remoteio.Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.gcsReader != nil {
		return r.gcsReader, nil
	}

	reader, closer, err := newStorageReader(ctx, func(ctx context.Context) (remoteio.ReadWriteFactory, error) {
		return gcs.New(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("GCSリーダーの生成に失敗: %w", err)
	}
	r.gcsReader = reader
	r.gcsCloser = closer

	return r.gcsReader, nil
}

// getS3Reader は、S3リーダーの取得と管理
func (r *UniversalReader) getS3Reader(ctx context.Context) (remoteio.Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.s3Reader != nil {
		return r.s3Reader, nil
	}

	reader, closer, err := newStorageReader(ctx, func(ctx context.Context) (remoteio.ReadWriteFactory, error) {
		return s3.New(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("S3リーダーの生成に失敗: %w", err)
	}
	r.s3Reader = reader
	r.s3Closer = closer

	return r.s3Reader, nil
}

// newStorageReader は、ストレージリーダーの生成とクロージャの管理
func newStorageReader(
	ctx context.Context,
	newFactory func(context.Context) (remoteio.ReadWriteFactory, error),
) (remoteio.Reader, io.Closer, error) {
	factory, err := newFactory(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("ストレージファクトリの生成に失敗: %w", err)
	}

	reader, err := factory.Reader()
	if err != nil {
		_ = factory.Close()
		return nil, nil, fmt.Errorf("リーダーの生成に失敗: %w", err)
	}

	return reader, factory, nil
}
