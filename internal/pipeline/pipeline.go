package pipeline

import (
	"context"

	"github.com/shouni/go-remote-io/remoteio"
)

// Pipeline はパイプラインの実行に必要な外部依存関係を保持するサービス構造体です。
type Pipeline struct {
	reader remoteio.Reader
}

// NewPipeline は、Pipeline を生成します。
func NewPipeline(reader remoteio.Reader) *Pipeline {
	return &Pipeline{
		reader: reader,
	}
}

// Execute は、すべての依存関係を構築し実行します。
func (p *Pipeline) Execute(
	ctx context.Context,
) (string, error) {
	// TODO: 実際のコンテンツ取得・変換ロジックを実装する
	return "", nil
}
