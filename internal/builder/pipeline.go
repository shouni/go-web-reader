package builder

import (
	"fmt"

	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/pipeline"
)

// buildPipeline は、提供されたランナーを使用して新しいパイプラインを初期化して返します。
func buildPipeline(cfg *config.Config, reader pipeline.ContentReader) (*pipeline.Pipeline, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	p, err := pipeline.NewPipeline(cfg.SourceURL, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pipeline: %w", err)
	}

	return p, nil
}
