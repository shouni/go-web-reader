package reader

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/shouni/go-http-kit/httpkit"
)

var supportedMediaTypes = []string{
	"text/html",
	"application/xhtml+xml",
	"text/plain",
	"text/markdown",
	"text/x-markdown",
}

type httpClientFetcher struct {
	client HTTPClient
}

// FetchBytes は HTTPClient を go-web-exact の ports.Fetcher として使うためのアダプタです。
func (f httpClientFetcher) FetchBytes(ctx context.Context, uri string) ([]byte, error) {
	req, err := newHTTPRequest(ctx, uri)
	if err != nil {
		return nil, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTPリクエスト失敗: %w", err)
	}
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	return httpkit.HandleResponse(resp)
}

// openHTTP は HTTP(S) URI を Content-Type ごとに処理して読み取りストリームを返します。
func (r *UniversalReader) openHTTP(ctx context.Context, uri string) (io.ReadCloser, error) {
	resp, err := r.fetchHTTP(ctx, uri)
	if err != nil {
		return nil, err
	}

	rawContentType := resp.Header.Get("Content-Type")
	contentType, err := mediaType(rawContentType)
	if err != nil {
		contentType = fallbackMediaType(rawContentType)
		if contentType == "" {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("Content-Typeの解析に失敗しました: %w", err)
		}
	}

	switch contentType {
	case "text/html", "application/xhtml+xml":
		return r.openExtractedHTML(ctx, uri, resp.Body)
	case "text/plain", "text/markdown", "text/x-markdown":
		return resp.Body, nil
	default:
		_ = resp.Body.Close()
		if contentType == "" {
			return nil, fmt.Errorf("未対応のContent-Typeです: %s", uri)
		}
		return nil, fmt.Errorf("未対応のContent-Typeです: %s (%s)", uri, contentType)
	}
}

// fetchHTTP は HTTP GET を実行し、成功レスポンスを返します。
func (r *UniversalReader) fetchHTTP(ctx context.Context, uri string) (*http.Response, error) {
	req, err := newHTTPRequest(ctx, uri)
	if err != nil {
		return nil, err
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTPリクエスト失敗: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("HTTPレスポンスがnilです")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("HTTPレスポンスボディがnilです")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("HTTPステータスエラー: %d", resp.StatusCode)
	}

	return resp, nil
}

// newHTTPRequest は reader 共通の HTTP GET リクエストを生成します。
func newHTTPRequest(ctx context.Context, uri string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTPリクエスト作成失敗: %w", err)
	}
	req.Header.Set("User-Agent", httpkit.UserAgent)
	req.Header.Set("Accept", strings.Join(supportedMediaTypes, ", "))

	return req, nil
}

// openExtractedHTML は取得済み HTML から本文テキストを抽出して読み取りストリームを返します。
func (r *UniversalReader) openExtractedHTML(ctx context.Context, uri string, body io.ReadCloser) (io.ReadCloser, error) {
	defer body.Close()

	text, hasBody, err := r.extractor.ExtractText(ctx, body)
	if err != nil {
		return nil, err
	}
	if !hasBody {
		return nil, fmt.Errorf("コンテンツが見つかりませんでした: %s", uri)
	}

	return io.NopCloser(strings.NewReader(text)), nil
}

// mediaType は Content-Type ヘッダーから media type だけを取り出して小文字化します。
func mediaType(contentType string) (string, error) {
	if contentType == "" {
		return "", nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	return mediaType, nil
}

// fallbackMediaType は不正な Content-Type ヘッダーから既知の media type を推定します。
func fallbackMediaType(contentType string) string {
	parts := strings.SplitN(contentType, ";", 2)
	normalized := strings.TrimSpace(strings.ToLower(parts[0]))
	for _, mediaType := range supportedMediaTypes {
		if normalized == mediaType {
			return mediaType
		}
	}
	return ""
}
