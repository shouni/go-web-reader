package builder

import (
	"context"

	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/pipeline"
)

// buildPipeline は、提供されたランナーを使用して新しいパイプラインを初期化して返します。
func buildPipeline(ctx context.Context, cfg *config.Config) (*pipeline.Pipeline, error) {
	_ = ctx
	p := pipeline.NewPipeline(cfg.SourceURL)

	return p, nil
}
