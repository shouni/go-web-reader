package builder

import (
	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/pipeline"
)

// buildPipeline は、提供されたランナーを使用して新しいパイプラインを初期化して返します。
func buildPipeline(cfg *config.Config, reader pipeline.ContentReader) (*pipeline.Pipeline, error) {
	p := pipeline.NewPipeline(cfg.SourceURL, reader)

	return p, nil
}
