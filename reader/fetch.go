package reader

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-http-kit/retry"
)

// fetched は 1 回の取得結果です。retry.RunValue が単一の値しか運べないため、
// ボディと Content-Type を 1 つにまとめています。
type fetched struct {
	body        []byte
	contentType string
}

// fetchBytes は URI を GET し、一時的な失敗は指数バックオフで再試行します。
//
// リトライをここで持つのは、HTTPClient の口が Do だけで「同じ GET をやり直す」判断を
// レスポンス 1 個からは下せないためです。既定の httpkit.Client も Do にはリトライを掛けません。
// httpkit.Get に委譲しないのは、あちらが自前でリクエストを組み立て newHTTPRequest の
// ヘッダーが失われるからです。
func (r *UniversalReader) fetchBytes(ctx context.Context, uri string) (fetched, error) {
	if r.retry.maxRetries == 0 {
		return r.fetchOnce(ctx, uri)
	}

	return retry.RunValue(ctx, func() (fetched, error) {
		return r.fetchOnce(ctx, uri)
	},
		retry.WithName("GET "+uri),
		retry.WithMaxRetries(r.retry.maxRetries),
		retry.WithInitialInterval(r.retry.initialInterval),
		retry.WithMaxInterval(r.retry.maxInterval),
		retry.WithShouldRetry(r.shouldRetryFetch),
	)
}

// fetchOnce は 1 度だけ GET します。使い終えた *http.Request は再送できないため、
// リクエストは呼ばれるたびに組み直します。
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

	// resp.Body の nil チェック・Close・サイズ上限は HandleResponse が行う（ここで Close すると二重になる）。
	body, err := httpkit.HandleResponse(resp)

	return fetched{body: body, contentType: contentType}, err
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

// shouldRetryFetch は、取得の失敗をやり直す価値があるかを判定します。
func (r *UniversalReader) shouldRetryFetch(err error) bool {
	if err == nil {
		return false
	}
	// 判断できるクライアントにはその判断を任せる（RetryClassifier 参照）。
	if classifier, ok := r.httpClient.(RetryClassifier); ok {
		return classifier.IsHTTPRetryableError(err)
	}

	// 呼び出し側が打ち切った操作を再開しても待ち時間が伸びるだけ。
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// 4xx やリクエスト・レスポンスの形の問題は、繰り返しても結果が変わらない。
	if httpkit.IsNonRetryableError(err) ||
		errors.Is(err, httpkit.ErrResponseBodyTooLarge) ||
		errors.Is(err, httpkit.ErrNilResponse) ||
		errors.Is(err, httpkit.ErrNilResponseBody) {
		return false
	}

	// 5xx / 408 / 429 と分類できない通信エラーは一時的な障害とみなす。
	return true
}
