package reader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"strings"
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

// mediaKinds は既知の media type と扱い方の対応表です。対応 Content-Type を
// 増やすときはここだけを更新します（分岐と壊れたヘッダーの救済の両方に効きます）。
var mediaKinds = map[string]mediaKind{
	"text/html":             mediaKindHTML,
	"application/xhtml+xml": mediaKindHTML,
	"text/plain":            mediaKindPassthrough,
	"text/markdown":         mediaKindPassthrough,
	"text/x-markdown":       mediaKindPassthrough,
	"text/csv":              mediaKindPassthrough,
	"application/json":      mediaKindPassthrough,
	"application/xml":       mediaKindPassthrough,
	"text/xml":              mediaKindPassthrough,
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

// openHTTP は HTTP(S) URI を Content-Type ごとに処理して読み取りストリームを返します。
// 取得を fetchBytes に一本化しているので、レスポンスサイズ上限はどの Content-Type にも等しくかかります。
func (r *UniversalReader) openHTTP(ctx context.Context, uri string) (io.ReadCloser, error) {
	got, err := r.fetchBytes(ctx, uri)
	if err != nil {
		return nil, err
	}

	contentType, err := resolveMediaType(got.contentType)
	if err != nil {
		return nil, fmt.Errorf("Content-Typeの解析に失敗しました: %w", err)
	}

	switch classifyMediaType(contentType) {
	case mediaKindHTML:
		// 抽出器には生のヘッダーを渡す。文字コードは charset パラメータ側にある。
		return r.openExtractedHTML(ctx, uri, bytes.NewReader(got.body), got.contentType)
	case mediaKindPassthrough:
		return io.NopCloser(bytes.NewReader(got.body)), nil
	default:
		if contentType == "" {
			return nil, fmt.Errorf("未対応のContent-Typeです: %s", uri)
		}
		return nil, fmt.Errorf("未対応のContent-Typeです: %s (%s)", uri, contentType)
	}
}

// openExtractedHTML は取得済み HTML から本文テキストを抽出して読み取りストリームを返します。
func (r *UniversalReader) openExtractedHTML(ctx context.Context, uri string, body io.Reader, contentType string) (io.ReadCloser, error) {
	text, hasBody, err := r.extractText(ctx, body, contentType)
	if err != nil {
		return nil, err
	}
	if !hasBody {
		return nil, fmt.Errorf("コンテンツが見つかりませんでした: %s", uri)
	}

	return io.NopCloser(strings.NewReader(text)), nil
}

// extractText は、抽出器が Content-Type を受け取れるならそれを添えて抽出します。
func (r *UniversalReader) extractText(ctx context.Context, body io.Reader, contentType string) (string, bool, error) {
	if extractor, ok := r.extractor.(ContentTypeExtractor); ok {
		return extractor.ExtractWithContentType(ctx, body, contentType)
	}
	return r.extractor.Extract(ctx, body)
}

// resolveMediaType は Content-Type ヘッダーから media type だけを取り出します。
//
// RFC に沿わないヘッダー（charset の引用符の閉じ忘れなど、実在するサーバーが返すもの）
// でも、";" より前が既知の media type なら採用します。未知のものまで救うと壊れたヘッダーを
// 根拠に中身を誤解釈するので、その場合は解析エラーを返します。
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
