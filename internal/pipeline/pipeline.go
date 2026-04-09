package pipeline

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Reader は URI からコンテンツを取得できる依存です。
type Reader interface {
	Read(ctx context.Context, uri string) (io.ReadCloser, error)
	io.Closer
}

// Pipeline はパイプラインの実行に必要な外部依存関係を保持するサービス構造体です。
type Pipeline struct {
	sourceURL string
	reader    Reader
}

// NewPipeline は、Pipeline を生成します。
func NewPipeline(sourceURL string, reader Reader) *Pipeline {
	return &Pipeline{
		sourceURL: sourceURL,
		reader:    reader,
	}
}

// Execute は、すべての依存関係を構築し実行します。
func (p *Pipeline) Execute(
	ctx context.Context,
) (string, error) {
	stream, err := p.reader.Read(ctx, p.sourceURL)
	if err != nil {
		return "", fmt.Errorf("failed to read source: %w", err)
	}
	defer func() {
		_ = stream.Close()
	}()

	body, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("failed to consume source: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}
