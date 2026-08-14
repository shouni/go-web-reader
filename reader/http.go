package reader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/shouni/go-http-kit/httpkit"
)

// mediaKind は media type から決まる本文の扱い方です。
type mediaKind int

const (
	// mediaKindUnsupported は本リーダーが扱わない media type です。
	mediaKindUnsupported mediaKind = iota
	// mediaKindHTML は本文抽出エンジンに通す media type です。
	mediaKindHTML
	// mediaKindPassthrough は変換せずそのまま返す media type です。
	mediaKindPassthrough
)

// mediaKinds は既知の media type と扱い方の対応表です。
// 対応 Content-Type を増やすときは、この表だけを更新すれば分岐と
// フォールバック判定の両方に反映されます。
var mediaKinds = map[string]mediaKind{
	"text/html":             mediaKindHTML,
	"application/xhtml+xml": mediaKindHTML,
	"text/plain":            mediaKindPassthrough,
	"text/markdown":         mediaKindPassthrough,
	"text/x-markdown":       mediaKindPassthrough,
}

// classifyMediaType は media type の扱い方を返します。
// image/* はサブタイプを問わずバイナリとしてそのまま通します。
func classifyMediaType(mediaType string) mediaKind {
	if kind, ok := mediaKinds[mediaType]; ok {
		return kind
	}
	if strings.HasPrefix(mediaType, "image/") {
		return mediaKindPassthrough
	}
	return mediaKindUnsupported
}

// fetchBytes は URI を GET し、ボディと Content-Type を返します。
func (r *UniversalReader) fetchBytes(ctx context.Context, uri string) ([]byte, string, error) {
	req, err := newHTTPRequest(ctx, uri)
	if err != nil {
		return nil, "", err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("HTTPリクエスト失敗: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")

	// resp.Body の nil チェックと Close は HandleResponse が内部で行うため、ここでは行わない
	// （二重 Close を避けるため）。
	body, err := httpkit.HandleResponse(resp)
	return body, contentType, err
}

// openHTTP は HTTP(S) URI を Content-Type ごとに処理して読み取りストリームを返します。
// 取得は Content-Type によらず fetchBytes に一本化しているため、
// httpkit.HandleResponse のレスポンスサイズ上限がどの Content-Type にも等しくかかります。
func (r *UniversalReader) openHTTP(ctx context.Context, uri string) (io.ReadCloser, error) {
	body, rawContentType, err := r.fetchBytes(ctx, uri)
	if err != nil {
		return nil, err
	}

	contentType, err := resolveMediaType(rawContentType)
	if err != nil {
		return nil, fmt.Errorf("Content-Typeの解析に失敗しました: %w", err)
	}

	switch classifyMediaType(contentType) {
	case mediaKindHTML:
		return r.openExtractedHTML(ctx, uri, bytes.NewReader(body))
	case mediaKindPassthrough:
		return io.NopCloser(bytes.NewReader(body)), nil
	default:
		if contentType == "" {
			return nil, fmt.Errorf("未対応のContent-Typeです: %s", uri)
		}
		return nil, fmt.Errorf("未対応のContent-Typeです: %s (%s)", uri, contentType)
	}
}

// newHTTPRequest は reader 共通の HTTP GET リクエストを生成します。
func newHTTPRequest(ctx context.Context, uri string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTPリクエスト作成失敗: %w", err)
	}
	req.Header.Set("User-Agent", httpkit.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", httpkit.AcceptLanguage)
	req.Header.Set("sec-ch-ua", httpkit.SecChUA)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	return req, nil
}

// openExtractedHTML は取得済み HTML から本文テキストを抽出して読み取りストリームを返します。
// body は取得済みバイト列を読むだけなので、クローズは不要です。
func (r *UniversalReader) openExtractedHTML(ctx context.Context, uri string, body io.Reader) (io.ReadCloser, error) {
	text, hasBody, err := r.extractor.Extract(ctx, body)
	if err != nil {
		return nil, err
	}
	if !hasBody {
		return nil, fmt.Errorf("コンテンツが見つかりませんでした: %s", uri)
	}

	return io.NopCloser(strings.NewReader(text)), nil
}

// resolveMediaType は Content-Type ヘッダーから media type だけを取り出します。
//
// ヘッダーが RFC に沿わず解析できない場合（charset の引用符を閉じ忘れているなど、
// 実在するサーバーが返してくるもの）でも、";" より前が既知の media type と一致すれば
// それを採用します。未知の media type まで救うと、壊れたヘッダーを根拠に
// 中身を誤って解釈することになるため、その場合は解析エラーをそのまま返します。
func resolveMediaType(contentType string) (string, error) {
	if contentType == "" {
		return "", nil
	}

	parsed, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		return parsed, nil
	}

	normalized := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.IndexByte(normalized, ';'); i >= 0 {
		normalized = strings.TrimSpace(normalized[:i])
	}
	if classifyMediaType(normalized) != mediaKindUnsupported {
		return normalized, nil
	}

	return "", err
}
