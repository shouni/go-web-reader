package reader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/netarmor/retry"
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

// fetched は 1 回の取得結果です。retry.RunValue が単一の値しか運べないため、
// ボディと Content-Type を 1 つにまとめています。
type fetched struct {
	body        []byte
	contentType string
}

// fetchBytes は URI を GET し、ボディと Content-Type を返します。
// 一時的な失敗（5xx / 408 / 429 / 通信エラー）は指数バックオフで再試行します。
//
// リトライを httpClient 側ではなくここで持つのは、HTTPClient の口が Do だけで、
// 「失敗したので同じ GET をやり直す」判断をレスポンス 1 個からは下せないためです。
// 既定の httpkit.Client も、Do を直接呼ぶ経路にはリトライを掛けません。
func (r *UniversalReader) fetchBytes(ctx context.Context, uri string) ([]byte, string, error) {
	if r.retry.maxRetries == 0 {
		got, err := r.fetchOnce(ctx, uri)
		return got.body, got.contentType, err
	}

	got, err := retry.RunValue(ctx, func() (fetched, error) {
		return r.fetchOnce(ctx, uri)
	},
		retry.WithName("GET "+uri),
		retry.WithMaxRetries(r.retry.maxRetries),
		retry.WithInitialInterval(r.retry.initialInterval),
		retry.WithMaxInterval(r.retry.maxInterval),
		retry.WithShouldRetry(r.shouldRetryFetch),
	)

	return got.body, got.contentType, err
}

// fetchOnce は再試行を挟まずに 1 度だけ GET します。
// リクエストは呼ばれるたびに組み直します。使い終えた *http.Request は
// ボディを読み切られている可能性があり、そのまま再送できません。
func (r *UniversalReader) fetchOnce(ctx context.Context, uri string) (fetched, error) {
	req, err := newHTTPRequest(ctx, uri)
	if err != nil {
		return fetched{}, err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fetched{}, fmt.Errorf("HTTPリクエスト失敗: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")

	// resp.Body の nil チェックと Close は HandleResponse が内部で行うため、ここでは行わない
	// （二重 Close を避けるため）。
	body, err := httpkit.HandleResponse(resp)

	return fetched{body: body, contentType: contentType}, err
}

// shouldRetryFetch は、取得の失敗をやり直す価値があるかを判定します。
func (r *UniversalReader) shouldRetryFetch(err error) bool {
	if err == nil {
		return false
	}
	// エラーの型を知っているのはそれを返したクライアントなので、
	// 判断できるクライアントにはその判断を任せます。
	if classifier, ok := r.httpClient.(RetryClassifier); ok {
		return classifier.IsHTTPRetryableError(err)
	}

	// 呼び出し側が打ち切った、あるいは期限切れの操作を再開しても待ち時間が伸びるだけです。
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// 4xx や、リクエスト・レスポンスの形そのものの問題は、繰り返しても結果が変わりません。
	if httpkit.IsNonRetryableError(err) ||
		errors.Is(err, httpkit.ErrResponseBodyTooLarge) ||
		errors.Is(err, httpkit.ErrNilResponse) ||
		errors.Is(err, httpkit.ErrNilResponseBody) {
		return false
	}

	// 5xx / 408 / 429 と、分類できない通信エラー（タイムアウトなど）は一時的な障害とみなします。
	return true
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
		// 抽出器には解析済みの media type ではなく生のヘッダーを渡します。
		// 文字コードは charset パラメータ側にあり、media type だけでは分かりません。
		return r.openExtractedHTML(ctx, uri, bytes.NewReader(body), rawContentType)
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
	req.Header.Set("sec-ch-ua-mobile", httpkit.SecChUAMobile)
	req.Header.Set("sec-ch-ua-platform", httpkit.SecChUAPlatform)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	return req, nil
}

// openExtractedHTML は取得済み HTML から本文テキストを抽出して読み取りストリームを返します。
// body は取得済みバイト列を読むだけなので、クローズは不要です。
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
