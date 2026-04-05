package builder

import (
	"context"
	"fmt"

	"github.com/shouni/go-remote-io/remoteio/gcs"

	"github.com/shouni/go-web-reader/internal/app"
)

// buildRemoteIO は、I/O コンポーネントを初期化します。
func buildRemoteIO(ctx context.Context) (*app.RemoteIO, error) {
	factory, err := gcs.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS factory: %w", err)
	}
	r, err := factory.Reader()
	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}

	return &app.RemoteIO{
		Factory: factory,
		Reader:  r,
	}, nil
}
