package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var (
	_ HTTPClient           = (*scriptedHTTPClient)(nil)
	_ RetryClassifier      = (*neverRetryClient)(nil)
	_ ContentTypeExtractor = (*contentTypeExtractorStub)(nil)
)

// response は scriptedHTTPClient が 1 回の Do で返す内容です。
type response struct {
	statusCode  int
	contentType string
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
	return resp, nil
}

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

// neverRetryClient は、自分の返すエラーを「やり直す価値なし」と判断するクライアントです。
type neverRetryClient struct {
	scriptedHTTPClient
}

func (c *neverRetryClient) IsHTTPRetryableError(error) bool { return false }

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

// contentTypeExtractorStub は Content-Type も受け取れる抽出器です。
type contentTypeExtractorStub struct {
	stubExtractor
	gotContentType string
	calls          int
}

func (s *contentTypeExtractorStub) ExtractWithContentType(ctx context.Context, r io.Reader, contentType string) (string, bool, error) {
	s.gotContentType = contentType
	s.calls++
	return s.Extract(ctx, r)
}

// 抽出器が Content-Type を受け取れるなら、解析済みの media type ではなく
// 生のヘッダーが渡ること。文字コードは charset パラメータ側にあります。
func TestExtractorReceivesRawContentType(t *testing.T) {
	t.Parallel()

	extractor := &contentTypeExtractorStub{stubExtractor: stubExtractor{text: "抽出結果", hasBody: true}}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: "text/html; charset=Shift_JIS",
		body:        "<html></html>",
	}))

	stream, err := r.Open(context.Background(), "https://example.com/sjis.html")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	if extractor.calls != 1 {
		t.Fatalf("ExtractWithContentType の呼び出し回数 = %d, want 1", extractor.calls)
	}
	if extractor.gotContentType != "text/html; charset=Shift_JIS" {
		t.Fatalf("contentType = %q, want %q", extractor.gotContentType, "text/html; charset=Shift_JIS")
	}
}

// ReadAll は開いて読み切って閉じるまでを行うこと。
func TestReadAllReadsWholeContent(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{}, WithHTTPClient(&stubHTTPClient{
		contentType: "text/plain",
		body:        "plain body",
	}))

	body, err := r.ReadAll(context.Background(), "https://example.com/a.txt")
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "plain body" {
		t.Fatalf("body = %q, want %q", got, "plain body")
	}
}

func TestReadAllPropagatesOpenError(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{}, WithHTTPClient(&stubHTTPClient{
		contentType: "application/json",
		body:        `{}`,
	}))

	if _, err := r.ReadAll(context.Background(), "https://example.com/a.json"); err == nil {
		t.Fatal("ReadAll() error = nil, want error")
	}
}

// Close は終端であり、解放するものが無い HTTP でも終端であること。
// 以前は storages 側にしか印が無く、Close 後も https:// だけ読めていました。
func TestOpenHTTPAfterCloseIsRejected(t *testing.T) {
	t.Parallel()

	client := &stubHTTPClient{contentType: "text/plain", body: "plain body"}
	r := newTestReader(t, &stubExtractor{}, WithHTTPClient(client))

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := r.Open(context.Background(), "https://example.com/a.txt"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Open() after Close error = %v, want ErrClosed", err)
	}
	if client.calls != 0 {
		t.Fatalf("client.calls = %d, want 0 (Close 後に取得してはいけない)", client.calls)
	}
}
