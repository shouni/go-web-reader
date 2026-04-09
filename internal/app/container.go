package app

import (
	"io"

	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/domain"
)

// Container はアプリケーションの依存関係（DIコンテナ）を保持します。
type Container struct {
	Config *config.Config
	// Business Logic
	Pipeline domain.Pipeline
	Closers  []io.Closer
}

// Close は、Container が保持するすべての外部接続リソースを安全に解放します。
func (c *Container) Close() {
	for _, closer := range c.Closers {
		if closer != nil {
			_ = closer.Close()
		}
	}
}
