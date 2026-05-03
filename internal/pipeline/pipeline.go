package pipeline

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// ContentReader は、指定されたURIからコンテンツを取得するためのインターフェースです。
type ContentReader interface {
	Open(ctx context.Context, uri string) (io.ReadCloser, error)
}

// Pipeline はパイプラインの実行に必要な外部依存関係を保持するサービス構造体です。
type Pipeline struct {
	sourceURL string
	reader    ContentReader
}

// NewPipeline は、Pipeline を生成します。
func NewPipeline(sourceURL string, reader ContentReader) *Pipeline {
	return &Pipeline{
		sourceURL: strings.TrimSpace(sourceURL),
		reader:    reader,
	}
}

// Execute は、すべての依存関係を構築し実行します。
func (p *Pipeline) Execute(
	ctx context.Context,
) (string, error) {
	if p == nil {
		return "", fmt.Errorf("pipeline is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	if p.sourceURL == "" {
		return "", fmt.Errorf("source URL is required")
	}
	if p.reader == nil {
		return "", fmt.Errorf("content reader is required")
	}

	stream, err := p.reader.Open(ctx, p.sourceURL)
	if err != nil {
		return "", fmt.Errorf("failed to read source: %w", err)
	}
	if stream == nil {
		return "", fmt.Errorf("content reader returned nil stream")
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
