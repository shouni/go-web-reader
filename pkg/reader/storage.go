package reader

import (
	"context"
	"fmt"
	"io"

	"github.com/shouni/go-remote-io/remoteio"
)

type storageReaderCache struct {
	reader remoteio.Reader
	closer io.Closer
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

// getGCSReader は GCS リーダーを遅延初期化して返します。
func (r *UniversalReader) getGCSReader(ctx context.Context) (remoteio.Reader, error) {
	return r.getStorageReader(ctx, &r.gcs, r.newGCSFactory, "GCS")
}

// getS3Reader は S3 リーダーを遅延初期化して返します。
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

// newStorageReader はストレージファクトリから入力リーダーとクローザーを生成します。
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
