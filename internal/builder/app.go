package builder

import (
	"fmt"

	"github.com/shouni/go-web-reader/internal/app"
	"github.com/shouni/go-web-reader/internal/config"
	"github.com/shouni/go-web-reader/internal/pipeline"
	pkgreader "github.com/shouni/go-web-reader/pkg/reader"
)

// BuildContainer は外部サービスとの接続を確立し、依存関係を組み立てた app.Container を返します。
func BuildContainer(cfg *config.Config) (container *app.Container, err error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	appCtx := &app.Container{
		Config: cfg,
	}

	defer func() {
		if err != nil {
			for _, c := range appCtx.Closers {
				_ = c.Close()
			}
		}
	}()

	// 1. Reader の初期化
	reader, err := pkgreader.New()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize reader: %w", err)
	}
	appCtx.Closers = append(appCtx.Closers, reader)

	// 2. Pipeline の構築
	p, err := pipeline.NewPipeline(cfg.SourceURL, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}
	appCtx.Pipeline = p

	return appCtx, nil
}
