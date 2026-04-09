package builder

import (
	"context"
	"fmt"

	"github.com/shouni/go-web-reader/internal/app"
	"github.com/shouni/go-web-reader/internal/config"
	pkgreader "github.com/shouni/go-web-reader/pkg/reader"
)

// BuildContainer は外部サービスとの接続を確立し、依存関係を組み立てた app.Container を返します。
func BuildContainer(ctx context.Context, cfg *config.Config) (container *app.Container, err error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	appCtx := &app.Container{
		Config: cfg,
	}

	reader, err := pkgreader.New()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize reader: %w", err)
	}
	appCtx.Closers = append(appCtx.Closers, reader)

	p, err := buildPipeline(cfg, reader)
	if err != nil {
		appCtx.Close()
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}
	appCtx.Pipeline = p

	return appCtx, nil
}
