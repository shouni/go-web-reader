// Package pipeline は、URIからのコンテンツ取得と抽出処理を実行するパイプラインを提供します。
package pipeline

import (
	"context"
	"fmt"
	"io"
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

// Execute は、設定されたソースからコンテンツを読み取り、w へ書き出します。
//
// 戻り値で文字列を返さないのは、読み取り結果を一度メモリに載せないためです。
// ContentReader が io.ReadCloser を返す設計なので、そのまま流せば
// gs:// 上の大きなファイルでも消費するメモリはコピーバッファ分で済みます。
func (p *Pipeline) Execute(ctx context.Context, w io.Writer) error {
	if p == nil {
		return fmt.Errorf("pipeline instance is nil")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}

	stream, err := p.reader.Open(ctx, p.sourceURL)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}
	if stream == nil {
		return fmt.Errorf("content reader returned nil stream")
	}
	defer func() { _ = stream.Close() }()

	if _, err := io.Copy(w, stream); err != nil {
		return fmt.Errorf("failed to consume source: %w", err)
	}

	return nil
}
