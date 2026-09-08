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
// ロックをスキームごとに持つのは、初期化に認証情報の解決などの I/O が伴うためです。
// 共有ロックだと GCS の初期化が詰まっている間、S3 の Open や Close まで待たされます。
//
// remoteio.Lazy では置き換えられません。ここで管理しているのは Factory の寿命
// （Close と解放後の利用拒否）で、Lazy は Handler を包むだけで寿命を持ちません。
type storageReaderCache struct {
	label      string
	newFactory StorageFactory

	mu     sync.Mutex
	reader remoteio.Reader
	closer io.Closer
	closed bool
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

	if c.closed {
		return nil, fmt.Errorf("%sリーダーは利用できません: %w", c.label, ErrClosed)
	}
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

	// 要るのは Open だけなので、保持する型は remoteio.Reader に絞る（読む以上のことをしないと型で示す）。
	store, err := factory.Store()
	if err == nil && store == nil {
		err = fmt.Errorf("store is nil")
	}
	if err != nil {
		// ファクトリはここでしか参照されないので、失敗したら閉じてから返す。
		_ = factory.Close()
		return nil, nil, fmt.Errorf("リーダーの生成に失敗: %w", err)
	}

	return store, factory, nil
}

// close は保持しているクローザーを閉じ、以後の利用を拒否します。
// io.Closer の慣習どおり終端です。解放後の Open が黙って初期化し直すと、
// 組み込んだ側からは「閉じたはずのものが接続を張り直す」ように見えます。
func (c *storageReaderCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.reader = nil

	if c.closer == nil {
		return nil
	}

	err := c.closer.Close()
	c.closer = nil

	return err
}
