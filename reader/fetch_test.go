package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-http-kit/retry"
)

var (
	_ HTTPClient      = (*scriptedHTTPClient)(nil)
	_ RetryClassifier = (*neverRetryClient)(nil)
)

// response は scriptedHTTPClient が 1 回の Do で返す内容です。
type response struct {
	statusCode  int
	contentType string
	retryAfter  string
	body        string
	err         error
}

// scriptedHTTPClient は呼ばれるたびに次の応答を返します。
// 台本を使い切ったあとは最後の応答を繰り返すため、「何回目で成功したか」を
// 数える用途でも、リトライ回数の上限を確かめる用途でも使えます。
type scriptedHTTPClient struct {
	script []response
	calls  int
}

func (c *scriptedHTTPClient) Do(*http.Request) (*http.Response, error) {
	res := c.script[min(c.calls, len(c.script)-1)]
	c.calls++

	if res.err != nil {
		return nil, res.err
	}
	statusCode := res.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(res.body)),
	}
	if res.contentType != "" {
		resp.Header.Set("Content-Type", res.contentType)
	}
	if res.retryAfter != "" {
		resp.Header.Set("Retry-After", res.retryAfter)
	}
	return resp, nil
}

// neverRetryClient は、自分の返すエラーを「やり直す価値なし」と判断するクライアントです。
type neverRetryClient struct {
	scriptedHTTPClient
}

func (c *neverRetryClient) IsHTTPRetryableError(error) bool { return false }

// newRetryingTestReader は、待ち時間をテスト向けに詰めたリーダーを返します。
func newRetryingTestReader(t *testing.T, client HTTPClient, opts ...Option) *UniversalReader {
	t.Helper()

	baseOpts := []Option{
		WithHTTPClient(client),
		WithRetryInterval(time.Millisecond, 2*time.Millisecond),
	}

	return newTestReader(t, &stubExtractor{text: "extracted", hasBody: true}, append(baseOpts, opts...)...)
}

// 一時的な失敗（5xx / 通信エラー）はやり直すこと。
// 既定のクライアントはリトライ付きで構築されますが、Do を直接呼ぶ経路には
// リトライが掛からないため、取得のやり直しは reader 側の責任です。
func TestFetchRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script []response
	}{
		{
			name: "server error",
			script: []response{
				{statusCode: http.StatusServiceUnavailable, body: "unavailable"},
				{statusCode: http.StatusBadGateway, body: "bad gateway"},
				{contentType: "text/plain", body: "recovered"},
			},
		},
		{
			name: "transport error",
			script: []response{
				{err: errors.New("connection reset by peer")},
				{err: errors.New("connection reset by peer")},
				{contentType: "text/plain", body: "recovered"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &scriptedHTTPClient{script: tt.script}
			r := newRetryingTestReader(t, client)

			body, err := r.ReadAll(context.Background(), "https://example.com/flaky")
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if got := string(body); got != "recovered" {
				t.Fatalf("body = %q, want %q", got, "recovered")
			}
			if client.calls != 3 {
				t.Fatalf("client.calls = %d, want 3", client.calls)
			}
		})
	}
}

// 繰り返しても結果が変わらない失敗はやり直さないこと。
func TestFetchDoesNotRetryPermanentFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "not found", statusCode: http.StatusNotFound},
		{name: "forbidden", statusCode: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &scriptedHTTPClient{script: []response{{statusCode: tt.statusCode, body: "nope"}}}
			r := newRetryingTestReader(t, client)

			if _, err := r.Open(context.Background(), "https://example.com/missing"); err == nil {
				t.Fatal("Open() error = nil, want error")
			}
			if client.calls != 1 {
				t.Fatalf("client.calls = %d, want 1", client.calls)
			}
		})
	}
}

// WithMaxRetries(0) はやり直しを止めること。
// 自前のリトライ付きクライアントを注入する利用者が、二重に待たされないための口です。
func TestFetchRetryCanBeDisabled(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{{statusCode: http.StatusServiceUnavailable, body: "unavailable"}}}
	r := newRetryingTestReader(t, client, WithMaxRetries(0))

	if _, err := r.Open(context.Background(), "https://example.com/flaky"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if client.calls != 1 {
		t.Fatalf("client.calls = %d, want 1", client.calls)
	}
}

// リトライ回数の上限を超えないこと（初回 + WithMaxRetries 回）。
func TestFetchStopsAtMaxRetries(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{{statusCode: http.StatusServiceUnavailable, body: "unavailable"}}}
	r := newRetryingTestReader(t, client, WithMaxRetries(3))

	if _, err := r.Open(context.Background(), "https://example.com/flaky"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if client.calls != 4 {
		t.Fatalf("client.calls = %d, want 4 (初回 + 3 回)", client.calls)
	}
}

// 待機時間の上限を超える Retry-After には従わず、待たずに打ち切ること。
// 叩く先は利用者が入力した URL なので、上限が無いと相手に待ち時間を決めさせることになります。
func TestFetchStopsOnRetryAfterBeyondMaxInterval(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{
		{statusCode: http.StatusTooManyRequests, retryAfter: "3600", body: "slow down"},
	}}
	r := newRetryingTestReader(t, client, WithMaxRetries(3), WithRetryInterval(time.Millisecond, 10*time.Millisecond))

	start := time.Now()
	_, err := r.Open(context.Background(), "https://example.com/throttled")
	if !errors.Is(err, retry.ErrRetryAfterTooLong) {
		t.Fatalf("Open() error = %v, want retry.ErrRetryAfterTooLong", err)
	}
	if client.calls != 1 {
		t.Errorf("client.calls = %d, want 1", client.calls)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("打ち切るべきところで %v 待ちました", waited)
	}
}

// 上限以内の Retry-After には従うこと。
func TestFetchHonorsRetryAfterWithinMaxInterval(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{
		{statusCode: http.StatusTooManyRequests, retryAfter: "1", body: "slow down"},
		{statusCode: http.StatusOK, contentType: "text/plain", body: "ok"},
	}}
	r := newRetryingTestReader(t, client, WithMaxRetries(3), WithRetryInterval(time.Millisecond, 2*time.Second))

	if _, err := r.Open(context.Background(), "https://example.com/throttled"); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if client.calls != 2 {
		t.Errorf("client.calls = %d, want 2", client.calls)
	}
}

// nilResponseClient は (nil, nil) を返す、規約を守らない HTTPClient です。
type nilResponseClient struct{ calls int }

func (c *nilResponseClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, nil
}

// 差し替えたクライアントが (nil, nil) を返しても panic せず、ErrNilResponse で
// 再試行なしに戻ること。ヘッダーを読む前に HandleResponse の nil チェックを通す配線です。
func TestFetchRejectsNilResponseWithoutPanic(t *testing.T) {
	t.Parallel()

	client := &nilResponseClient{}
	r := newRetryingTestReader(t, client, WithMaxRetries(3))

	_, err := r.Open(context.Background(), "https://example.com/")
	if !errors.Is(err, httpkit.ErrNilResponse) {
		t.Fatalf("Open() error = %v, want httpkit.ErrNilResponse", err)
	}
	if client.calls != 1 {
		t.Errorf("client.calls = %d, want 1（形の問題は再試行しない）", client.calls)
	}
}

// クライアント自身がリトライ可否を判断できるなら、その判断に従うこと。
// エラーの型を知っているのはそれを返したクライアントです。
func TestFetchDefersToClientRetryClassification(t *testing.T) {
	t.Parallel()

	client := &neverRetryClient{
		scriptedHTTPClient: scriptedHTTPClient{
			script: []response{{statusCode: http.StatusServiceUnavailable, body: "unavailable"}},
		},
	}
	r := newRetryingTestReader(t, client)

	if _, err := r.Open(context.Background(), "https://example.com/flaky"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if client.calls != 1 {
		t.Fatalf("client.calls = %d, want 1", client.calls)
	}
}
