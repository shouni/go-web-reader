package builder

import (
	"context"
	"fmt"
	"io"

	"github.com/shouni/go-web-reader/internal/app"
	"github.com/shouni/go-web-reader/internal/config"

	"github.com/shouni/go-http-kit/httpkit"
)

// BuildContainer は外部サービスとの接続を確立し、依存関係を組み立てた app.Container を返します。
func BuildContainer(ctx context.Context, cfg *config.Config) (container *app.Container, err error) {
	var resources []io.Closer
	defer func() {
		if err != nil {
			for _, r := range resources {
				if r != nil {
					_ = r.Close()
				}
			}
		}
	}()

	// 1. HttpClient (全アダプターの基盤)
	httpClient := httpkit.New(config.DefaultHTTPTimeout)

	// 2. I/O Infrastructure (マルチクラウクラウド対応)
	rio, err := buildRemoteIO(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize IO components: %w", err)
	}
	resources = append(resources, rio)

	appCtx := &app.Container{
		Config:     cfg,
		RemoteIO:   rio,
		HTTPClient: httpClient,
	}

	return appCtx, nil
}
