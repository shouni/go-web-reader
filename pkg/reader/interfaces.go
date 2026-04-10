package reader

import (
	"context"
	"io"
)

// ContentReader は、テキストやメッセージの生成に特化した最小のインターフェースです。
type ContentReader interface {
	Open(ctx context.Context, uri string) (io.ReadCloser, error)
	io.Closer
}
