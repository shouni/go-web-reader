package pipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	pkgreader "github.com/shouni/go-web-reader/pkg/reader"
)

// Pipeline はパイプラインの実行に必要な外部依存関係を保持するサービス構造体です。
type Pipeline struct {
	sourceURL string
}

// NewPipeline は、Pipeline を生成します。
func NewPipeline(sourceURL string) *Pipeline {
	return &Pipeline{
		sourceURL: sourceURL,
	}
}

// Execute は、すべての依存関係を構築し実行します。
func (p *Pipeline) Execute(
	ctx context.Context,
) (string, error) {
	reader, err := pkgreader.New()
	if err != nil {
		return "", fmt.Errorf("failed to initialize reader: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	stream, err := reader.Read(ctx, p.sourceURL)
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
