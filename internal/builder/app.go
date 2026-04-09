package builder

import (
	"context"
	"fmt"

	"github.com/shouni/go-web-reader/internal/app"
	"github.com/shouni/go-web-reader/internal/config"
)

// BuildContainer は外部サービスとの接続を確立し、依存関係を組み立てた app.Container を返します。
func BuildContainer(ctx context.Context, cfg *config.Config) (container *app.Container, err error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	appCtx := &app.Container{
		Config: cfg,
	}

	p, err := buildPipeline(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}
	appCtx.Pipeline = p

	return appCtx, nil
}
