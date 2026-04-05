package reader

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
	"github.com/shouni/go-web-exact/v2/extract"
	"github.com/shouni/go-web-exact/v2/ports"
	"github.com/shouni/netarmor/securenet"
)

// UniversalReader はあらゆるURIからデータを読み取るインターフェース
type UniversalReader struct {
	// 内部で各プロトコルのハンドラを保持
	reader    remoteio.Reader
	extractor ports.Extractor
}

// New はUniversalReaderの新しいインスタンスを生成します。
func New(ctx context.Context, uri string) (*UniversalReader, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if uri == "" {
		return nil, fmt.Errorf("uri cannot be empty")
	}

	ok, err := securenet.IsSafeURL(uri)
	if ok == false {
		return nil, err
	}

	universalReader := UniversalReader{}

	if strings.HasPrefix(uri, securenet.SchemeHTTPS) {
		// HTTP/Web 抽出の処理
		httpClient := httpkit.New(httpkit.DefaultHTTPTimeout)
		extractor, err := extract.NewExtractor(httpClient)
		if err != nil {
			return nil, fmt.Errorf("Extractorの初期化エラー: %w", err)
		}
		universalReader.extractor = extractor
	}

	// クラウドストレージ
	reader, err := ioReader(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("リーダーの生成に失敗: %w", err)
	}
	universalReader.reader = reader

	return &universalReader, nil
}

// Open は URI のスキームを判別し、適切なリーダーを返します
func (r *UniversalReader) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		// 1. HTTP/Web 抽出の処理
		httpClient := httpkit.New(httpkit.DefaultHTTPTimeout)
		extractor, err := extract.NewExtractor(httpClient)
		if err != nil {
			return nil, fmt.Errorf("Extractorの初期化エラー: %w", err)
		}

		// text (string) を取得
		text, hasBody, err := extractor.FetchAndExtractText(ctx, uri)
		if err != nil {
			return nil, err
		}
		if !hasBody {
			return nil, fmt.Errorf("コンテンツが見つかりませんでした: %s", uri)
		}

		// string を io.ReadCloser (NopCloser) に変換して返す
		return io.NopCloser(strings.NewReader(text)), nil
	}

	reader, err := ioReader(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("リーダーの生成に失敗: %w", err)
	}

	return reader.Open(ctx, uri)
}

// ioReader は、URIスキーム（例：GCS、S3）に基づいて適切なremoteio.Readerを解決するか、エラーを返します。
func ioReader(ctx context.Context, uri string) (remoteio.Reader, error) {
	// 2. クラウドストレージ / ローカルの処理
	var factory remoteio.ReadWriteFactory
	var err error

	if remoteio.IsGCSURI(uri) {
		factory, err = gcs.New(ctx)
	} else if remoteio.IsS3URI(uri) {
		factory, err = s3.New(ctx)
	} else {
		// local 等、他のスキームのフォールバック処理が必要な場合はここに記述
		return nil, fmt.Errorf("未対応のURIスキームです: %s", uri)
	}

	if err != nil {
		return nil, fmt.Errorf("ストレージファクトリの生成に失敗: %w", err)
	}

	reader, err := factory.Reader()
	if err != nil {
		return nil, fmt.Errorf("リーダーの生成に失敗: %w", err)
	}

	return reader, nil
}
