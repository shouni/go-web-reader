package reader

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/shouni/go-remote-io/remoteio"
)

// storageReaderCache は 1 スキーム分のストレージリーダーを遅延初期化して保持します。
//
// ロックをリーダー本体ではなくキャッシュごとに持たせているのは、初期化に
// 認証情報の解決などの I/O が伴うためです。共有ロックだと GCS の初期化が
// 詰まっている間、S3 の Open や Close まで待たされます。
type storageReaderCache struct {
	label      string
	newFactory StorageFactory

	mu     sync.Mutex
	reader remoteio.Reader
	closer io.Closer
}

// openStorage はスキームに対応するストレージリーダーを取得し、URI の読み取りストリームを返します。
func (r *UniversalReader) openStorage(ctx context.Context, uri string, cache *storageReaderCache) (io.ReadCloser, error) {
	reader, err := cache.get(ctx)
	if err != nil {
		return nil, err
	}

	return reader.Open(ctx, uri)
}

// get はストレージリーダーを遅延初期化し、以後の呼び出しでは同じものを再利用します。
func (c *storageReaderCache) get(ctx context.Context) (remoteio.Reader, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.reader != nil {
		return c.reader, nil
	}

	reader, closer, err := newStorageReader(ctx, c.newFactory)
	if err != nil {
		return nil, fmt.Errorf("%sリーダーの生成に失敗: %w", c.label, err)
	}
	c.reader = reader
	c.closer = closer

	return c.reader, nil
}

// newStorageReader はストレージファクトリから入力リーダーとクローザーを生成します。
func newStorageReader(ctx context.Context, newFactory StorageFactory) (remoteio.Reader, io.Closer, error) {
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
// 解放後に再度 Open された場合は、次の get で初期化からやり直されます。
func (c *storageReaderCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closer == nil {
		c.reader = nil
		return nil
	}

	err := c.closer.Close()
	c.reader = nil
	c.closer = nil

	return err
}
