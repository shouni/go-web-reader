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
func NewPipeline(sourceURL string, reader ContentReader) (*Pipeline, error) {
	if sourceURL == "" {
		return nil, fmt.Errorf("source URL is required")
	}
	if reader == nil {
		return nil, fmt.Errorf("content reader is required")
	}

	return &Pipeline{
		sourceURL: sourceURL,
		reader:    reader,
	}, nil
}

// Execute は、設定されたソースからコンテンツを読み取り、実行結果を返します。
func (p *Pipeline) Execute(ctx context.Context) (string, error) {
	if p == nil {
		return "", fmt.Errorf("pipeline instance is nil")
	}
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}

	stream, err := p.reader.Open(ctx, p.sourceURL)
	if err != nil {
		return "", fmt.Errorf("failed to read source: %w", err)
	}
	if stream == nil {
		return "", fmt.Errorf("content reader returned nil stream")
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("failed to consume source: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}
