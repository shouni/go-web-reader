package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
)

// 依存ライブラリ側のインターフェースが変わったとき、スタブの追従漏れを
// テスト実行時ではなくビルド時に検出するためのアサーション。
// （go-remote-io v1.7.0 の List への ListOption 追加のような変更を取りこぼさないため）
var (
	_ Extractor  = (*stubExtractor)(nil)
	_ HTTPClient = (*stubHTTPClient)(nil)
	_ HTTPClient = (*headerOverridingClient)(nil)
)

type stubExtractor struct {
	text          string
	hasBody       bool
	err           error
	extractedBody string
	extractCalls  int
}

func (s *stubExtractor) Extract(_ context.Context, reader io.Reader) (string, bool, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", false, err
	}
	s.extractedBody = string(body)
	s.extractCalls++
	return s.text, s.hasBody, s.err
}

type stubHTTPClient struct {
	contentType string
	body        string
	statusCode  int
	err         error
	lastReq     *http.Request
	calls       int
}

func (s *stubHTTPClient) Do(req *http.Request) (*http.Response, error) {
	s.lastReq = req
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	statusCode := s.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}
	if s.contentType != "" {
		resp.Header.Set("Content-Type", s.contentType)
	}
	return resp, nil
}

// headerOverridingClient は、既定のヘッダーを上書きしてから委譲するクライアントです。
type headerOverridingClient struct {
	inner     HTTPClient
	userAgent string
}

func (c *headerOverridingClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	return c.inner.Do(req)
}

func newTestReader(t *testing.T, extractor Extractor, opts ...Option) *UniversalReader {
	t.Helper()

	baseOpts := []Option{
		WithExtractor(extractor),
		WithSafeURLValidator(func(context.Context, string) error { return nil }),
	}
	baseOpts = append(baseOpts, opts...)

	return New(baseOpts...)
}

func TestNewAcceptsDoOnlyHTTPClientWithDefaultExtractor(t *testing.T) {
	t.Parallel()

	r := New(
		WithHTTPClient(&stubHTTPClient{}),
		WithSafeURLValidator(func(context.Context, string) error { return nil }),
	)
	if r.extractor == nil {
		t.Fatal("既定の抽出器が設定されていない")
	}
}

// nil を渡したオプションは無視され、既定値が保たれること。
func TestNilOptionsAreIgnored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  Option
	}{
		{name: "nil の Option そのもの", opt: nil},
		{name: "safe URL validator", opt: WithSafeURLValidator(nil)},
		{name: "GCS factory", opt: WithGCSFactory(nil)},
		{name: "S3 factory", opt: WithS3Factory(nil)},
		{name: "HTTP client", opt: WithHTTPClient(nil)},
		{name: "extractor", opt: WithExtractor(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := New(WithExtractor(&stubExtractor{}), tt.opt)
			if r.safeURL == nil || r.extractor == nil || r.httpClient == nil {
				t.Fatal("nil オプションで既定の依存が失われている")
			}
			for _, scheme := range []string{remoteio.SchemeGCS, remoteio.SchemeS3} {
				if storageCache(t, r, scheme).newFactory == nil {
					t.Fatalf("%s のファクトリが失われている", scheme)
				}
			}
		})
	}
}

// リクエストのヘッダーは WithHTTPClient から差し替えられること。
// 取得処理を丸ごと差し替える口は無いので、ヘッダーを変えたい利用者はこの経路に頼る。
func TestWithHTTPClientCanOverrideRequestHeaders(t *testing.T) {
	t.Parallel()

	inner := &stubHTTPClient{contentType: "text/plain", body: "fetched"}
	r := newTestReader(t, &stubExtractor{},
		WithHTTPClient(&headerOverridingClient{inner: inner, userAgent: "custom-agent"}),
	)

	stream, err := r.Open(context.Background(), "https://example.com/a.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "fetched" {
		t.Fatalf("body = %q, want %q", got, "fetched")
	}
	if got := inner.lastReq.Header.Get("User-Agent"); got != "custom-agent" {
		t.Fatalf("User-Agent = %q, want %q", got, "custom-agent")
	}
}

func TestOpenRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	safeCheckErr := errors.New("lookup failed")
	tests := []struct {
		name string
		ctx  context.Context
		uri  string
		opts []Option
	}{
		{name: "nil context", ctx: nil, uri: "https://example.com"},
		{name: "empty uri", ctx: context.Background(), uri: ""},
		{name: "unsafe uri", ctx: context.Background(), uri: "https://example.com/private", opts: []Option{WithSafeURLValidator(func(context.Context, string) error { return errors.New("unsafe") })}},
		{name: "safe checker error", ctx: context.Background(), uri: "https://example.com/private", opts: []Option{WithSafeURLValidator(func(context.Context, string) error { return safeCheckErr })}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newTestReader(t, &stubExtractor{}, tt.opts...)

			_, err := r.Open(tt.ctx, tt.uri)
			if err == nil {
				t.Fatal("Open() error = nil, want error")
			}
		})
	}
}

func TestOpenRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{})

	_, err := r.Open(context.Background(), "ftp://example.com/file.txt")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "未対応のURIスキームです") {
		t.Fatalf("Open() error = %v", err)
	}
}

func TestOpenWrapsSafeURLValidatorError(t *testing.T) {
	t.Parallel()

	safeCheckErr := errors.New("lookup failed")
	r := newTestReader(t, &stubExtractor{}, WithSafeURLValidator(func(context.Context, string) error {
		return safeCheckErr
	}))

	_, err := r.Open(context.Background(), "https://example.com/private")
	if !errors.Is(err, safeCheckErr) {
		t.Fatalf("Open() error = %v, want wrapping %v", err, safeCheckErr)
	}
}

// URL 安全性検証は HTTP(S) にだけ掛かること。
// 接続先をクラウド SDK が決める gs:// / s3:// に対しては、検証する相手がいません。
func TestSafeURLValidatorRunsOnlyForHTTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		uri        string
		wantCalled bool
	}{
		{name: "https", uri: "https://example.com/a.txt", wantCalled: true},
		{name: "http", uri: "http://example.com/a.txt", wantCalled: true},
		{name: "gs", uri: "gs://bucket/path.txt"},
		{name: "s3", uri: "s3://bucket/path.txt"},
		{name: "unsupported", uri: "ftp://example.com/a.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var validated []string
			r := New(
				WithExtractor(&stubExtractor{}),
				WithHTTPClient(&stubHTTPClient{contentType: "text/plain", body: "body"}),
				WithGCSFactory(func(context.Context) (remoteio.Factory, error) {
					return &stubFactory{reader: &stubReader{content: "body"}}, nil
				}),
				WithS3Factory(func(context.Context) (remoteio.Factory, error) {
					return &stubFactory{reader: &stubReader{content: "body"}}, nil
				}),
				WithSafeURLValidator(func(_ context.Context, uri string) error {
					validated = append(validated, uri)
					return nil
				}),
			)
			defer func() { _ = r.Close() }()

			if stream, err := r.Open(context.Background(), tt.uri); err == nil {
				_ = stream.Close()
			}

			if called := len(validated) > 0; called != tt.wantCalled {
				t.Fatalf("検証器の呼び出し = %v (%v), want %v", called, validated, tt.wantCalled)
			}
		})
	}
}

// 解放するものが無い HTTP でも Close は終端であること。
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
		contentType: "application/octet-stream",
		body:        `{}`,
	}))

	if _, err := r.ReadAll(context.Background(), "https://example.com/a.json"); err == nil {
		t.Fatal("ReadAll() error = nil, want error")
	}
}
