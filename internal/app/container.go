// Package app は、アプリケーションの依存関係を組み立てて保持する DI コンテナを提供します。
package app

import (
	"io"

	"github.com/shouni/go-web-reader/internal/closeutil"
	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/pipeline"
)

// Container はアプリケーションの依存関係（DIコンテナ）を保持します。
type Container struct {
	Config *config.Config
	// Pipeline は取得と出力の実処理です。実装が 1 つしかなく、差し替える側も
	// いないため、インターフェースを挟まず具象で保持します。テストで差し替えたいのは
	// その内側の ContentReader で、そちらは pipeline パッケージが受け口を持っています。
	Pipeline *pipeline.Pipeline
	Closers  []io.Closer
}

// Close は、Container が保持するすべての外部接続リソースを安全に解放します。
func (c *Container) Close() error {
	if c == nil {
		return nil
	}

	fns := make([]func() error, 0, len(c.Closers))
	for _, closer := range c.Closers {
		if closer != nil {
			fns = append(fns, closer.Close)
		}
	}

	return closeutil.Join(fns...)
}
