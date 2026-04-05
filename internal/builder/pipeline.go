package builder

import (
	"context"

	"github.com/shouni/go-web-reader/internal/app"
	"github.com/shouni/go-web-reader/internal/pipeline"
)

// buildPipeline は、提供されたランナーを使用して新しいパイプラインを初期化して返します。
func buildPipeline(ctx context.Context, appCtx *app.Container) (*pipeline.Pipeline, error) {
	p := pipeline.NewPipeline(appCtx.RemoteIO.Reader)

	return p, nil
}
