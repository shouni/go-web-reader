package app

import (
	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/domain"
)

// Container はアプリケーションの依存関係（DIコンテナ）を保持します。
type Container struct {
	Config *config.Config
	// Business Logic
	Pipeline domain.Pipeline
}

// Close は、Container が保持するすべての外部接続リソースを安全に解放します。
func (c *Container) Close() {
}
