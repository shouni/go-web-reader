package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shouni/go-remote-io/remoteio"
)

// --- Stubs ---

// 依存ライブラリ側のインターフェースが変わったとき、スタブの追従漏れを
// テスト実行時ではなくビルド時に検出するためのアサーション。
// （go-remote-io v1.7.0 の List への ListOption 追加のような変更を取りこぼさないため）
var (
	_ remoteio.InputReader = (*stubReader)(nil)
	_ remoteio.IOFactory   = (*stubFactory)(nil)
	_ Extractor            = (*stubExtractor)(nil)
	_ HTTPClient           = (*stubHTTPClient)(nil)
	_ HTTPClient           = (*headerOverridingClient)(nil)
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

// remoteio.InputReader を満足させるスタブ
type stubReader struct {
	content  string
	err      error
	lastPath string
}

func (s *stubReader) Open(_ context.Context, path string) (io.ReadCloser, error) {
	s.lastPath = path
	if s.err != nil {
		return nil, s.err
	}
	return io.NopCloser(strings.NewReader(s.content)), nil
}

// Lister / Exister インターフェースの実装（必要に応じて）
func (s *stubReader) List(_ context.Context, _ string, _ func(string) error, _ ...remoteio.ListOption) error {
	return nil
}
func (s *stubReader) Exists(_ context.Context, _ string) (bool, error) { return true, nil }

type stubCloser struct {
	closed int
	err    error
}

func (s *stubCloser) Close() error {
	s.closed++
	return s.err
}

// remoteio.IOFactory を満足させるスタブ
type stubFactory struct {
	reader     remoteio.InputReader // 指標：具象型ではなくインターフェースで保持するように変更
	readerErr  error
	closeErr   error
	closeCalls int
}

func (s *stubFactory) InputReader() (remoteio.InputReader, error) {
	if s.readerErr != nil {
		return nil, s.readerErr
	}
	// ここが nil であれば、呼び出し側で reader == nil として正しく判定されるのだ
	return s.reader, nil
}

func (s *stubFactory) OutputWriter() (remoteio.OutputWriter, error) { return nil, nil }
func (s *stubFactory) URLSigner() (remoteio.URLSigner, error)       { return nil, nil }

func (s *stubFactory) Close() error {
	s.closeCalls++
	return s.closeErr
}

// --- Tests ---

func TestReadHTTPUsesExtractor(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "hello world", hasBody: true}
	httpClient := &stubHTTPClient{contentType: "text/html; charset=utf-8", body: "<html></html>"}
	r := newTestReader(t, extractor, WithHTTPClient(httpClient))

	stream, err := r.Open(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "hello world" {
		t.Fatalf("body = %q, want %q", got, "hello world")
	}
	if extractor.extractCalls != 1 {
		t.Fatalf("extractor.extractCalls = %d, want 1", extractor.extractCalls)
	}
	if extractor.extractedBody != "<html></html>" {
		t.Fatalf("extractor.extractedBody = %q", extractor.extractedBody)
	}
	if httpClient.calls != 1 {
		t.Fatalf("httpClient.calls = %d, want 1", httpClient.calls)
	}
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

func TestReadHTTPFallsBackForMalformedContentType(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "fallback text", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: `text/html; charset="`,
		body:        "<html>fallback</html>",
	}))

	stream, err := r.Open(context.Background(), "https://example.com/malformed-content-type")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "fallback text" {
		t.Fatalf("body = %q, want %q", got, "fallback text")
	}
	if extractor.extractCalls != 1 {
		t.Fatalf("extractor.extractCalls = %d, want 1", extractor.extractCalls)
	}
}

func TestReadHTTPMalformedContentTypeDoesNotFallbackOnPartialMatch(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "unexpected", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: `text/html-sandboxed; charset="`,
		body:        "<html>unexpected</html>",
	}))

	_, err := r.Open(context.Background(), "https://example.com/bad-content-type")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "Content-Typeの解析に失敗しました") {
		t.Fatalf("Open() error = %v", err)
	}
	if extractor.extractCalls != 0 {
		t.Fatalf("extractor.extractCalls = %d, want 0", extractor.extractCalls)
	}
}

func TestReadHTTPNoBodyReturnsError(t *testing.T) {
	t.Parallel()

	r := newTestReader(t,
		&stubExtractor{hasBody: false},
		WithHTTPClient(&stubHTTPClient{contentType: "application/xhtml+xml"}),
	)

	_, err := r.Open(context.Background(), "https://example.com/empty")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "コンテンツが見つかりませんでした") {
		t.Fatalf("Open() error = %v", err)
	}
}

func TestReadHTTPPlainTextReturnsBodyWithoutExtractor(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "html text", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: "text/plain; charset=utf-8",
		body:        "plain body",
	}))

	stream, err := r.Open(context.Background(), "https://example.com/plain.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "plain body" {
		t.Fatalf("body = %q, want %q", got, "plain body")
	}
	if extractor.extractCalls != 0 {
		t.Fatalf("extractor.extractCalls = %d, want 0", extractor.extractCalls)
	}
}

func TestReadHTTPMarkdownReturnsBodyWithoutExtractor(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "html text", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: "text/markdown; charset=utf-8",
		body:        "# Title\n\nmarkdown body",
	}))

	stream, err := r.Open(context.Background(), "https://example.com/README.md")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "# Title\n\nmarkdown body" {
		t.Fatalf("body = %q", got)
	}
	if extractor.extractCalls != 0 {
		t.Fatalf("extractor.extractCalls = %d, want 0", extractor.extractCalls)
	}
}

func TestReadHTTPImageReturnsBodyWithoutExtractor(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "html text", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: "image/png",
		body:        "fake-png-bytes",
	}))

	stream, err := r.Open(context.Background(), "https://example.com/photo.png")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "fake-png-bytes" {
		t.Fatalf("body = %q, want %q", got, "fake-png-bytes")
	}
	if extractor.extractCalls != 0 {
		t.Fatalf("extractor.extractCalls = %d, want 0", extractor.extractCalls)
	}
}

func TestReadHTTPUnsupportedContentTypeReturnsError(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "html text", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: "application/json",
		body:        `{"message":"nope"}`,
	}))

	_, err := r.Open(context.Background(), "https://example.com/data.json")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "未対応のContent-Type") {
		t.Fatalf("Open() error = %v", err)
	}
	if extractor.extractCalls != 0 {
		t.Fatalf("extractor.extractCalls = %d, want 0", extractor.extractCalls)
	}
}

func TestReadGCSUsesInjectedReader(t *testing.T) {
	t.Parallel()

	storageReader := &stubReader{content: "gcs body"}
	r := newTestReader(t, &stubExtractor{})
	storageCache(t, r, remoteio.PrefixGCS).reader = storageReader

	stream, err := r.Open(context.Background(), "gs://bucket/path.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "gcs body" {
		t.Fatalf("body = %q, want %q", got, "gcs body")
	}
	if storageReader.lastPath != "gs://bucket/path.txt" {
		t.Fatalf("reader.lastPath = %q", storageReader.lastPath)
	}
}

func TestReadS3UsesInjectedReader(t *testing.T) {
	t.Parallel()

	storageReader := &stubReader{content: "s3 body"}
	r := newTestReader(t, &stubExtractor{})
	storageCache(t, r, remoteio.PrefixS3).reader = storageReader

	stream, err := r.Open(context.Background(), "s3://bucket/path.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "s3 body" {
		t.Fatalf("body = %q, want %q", got, "s3 body")
	}
	if storageReader.lastPath != "s3://bucket/path.txt" {
		t.Fatalf("reader.lastPath = %q", storageReader.lastPath)
	}
}

func TestReadRejectsInvalidInput(t *testing.T) {
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

func TestReadWrapsSafeCheckerError(t *testing.T) {
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

// nil を渡したオプションは無視され、既定値が保たれること。
//
// 以前は New 側で「既定値が入っているはずのフィールドが nil でないか」を検証して
// いましたが、それは明示的に nil を渡したときにしか到達しない分岐でした。
// 入口で弾く形にして、必須依存が欠けた状態を作れないようにしています。
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
			for _, scheme := range []string{remoteio.PrefixGCS, remoteio.PrefixS3} {
				if storageCache(t, r, scheme).newFactory == nil {
					t.Fatalf("%s のファクトリが失われている", scheme)
				}
			}
		})
	}
}

// リクエストのヘッダーは WithHTTPClient から差し替えられること。
// 取得処理そのものを差し替える口（旧 WithFetcher）を持たないぶん、
// ヘッダーを変えたい利用者はこの経路に頼ります。
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

// headerOverridingClient は、既定のヘッダーを上書きしてから委譲するクライアントです。
type headerOverridingClient struct {
	inner     HTTPClient
	userAgent string
}

func (c *headerOverridingClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	return c.inner.Do(req)
}

func TestCloseClosesManagedResources(t *testing.T) {
	t.Parallel()

	gcsCloser := &stubCloser{}
	s3Closer := &stubCloser{}
	r := newTestReader(t, &stubExtractor{})
	gcsCache := storageCache(t, r, remoteio.PrefixGCS)
	s3Cache := storageCache(t, r, remoteio.PrefixS3)
	gcsCache.reader = &stubReader{}
	gcsCache.closer = gcsCloser
	s3Cache.reader = &stubReader{}
	s3Cache.closer = s3Closer

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if gcsCloser.closed != 1 || s3Closer.closed != 1 {
		t.Fatalf("close counts = (%d, %d), want (1, 1)", gcsCloser.closed, s3Closer.closed)
	}
	if gcsCache.reader != nil || gcsCache.closer != nil || s3Cache.reader != nil || s3Cache.closer != nil {
		t.Fatal("managed resources were not cleared")
	}
}

// Close は終端であり、解放後の Open は ErrClosed になること。
// 以前は黙って初期化からやり直しており、組み込んだ側からは
// 「閉じたはずのものが接続を張り直す」ように見えていました。
func TestOpenAfterCloseIsRejected(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{})
	storageCache(t, r, remoteio.PrefixGCS).reader = &stubReader{content: "x"}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err := r.Open(context.Background(), "gs://bucket/path.txt")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Open() after Close error = %v, want ErrClosed", err)
	}
}

// storageCache は、スキームに対応するキャッシュをテストから取り出します。
func storageCache(t *testing.T, r *UniversalReader, scheme string) *storageReaderCache {
	t.Helper()

	cache, ok := r.storages[scheme]
	if !ok {
		t.Fatalf("スキーム %q のストレージが登録されていません", scheme)
	}
	return cache
}

func TestNewStorageReaderClosesFactoryOnReaderError(t *testing.T) {
	t.Parallel()

	factory := &stubFactory{
		readerErr: errors.New("reader failed"),
	}

	_, _, err := newStorageReader(context.Background(), func(context.Context) (remoteio.IOFactory, error) {
		return factory, nil
	})
	if err == nil {
		t.Fatal("newStorageReader() error = nil, want error")
	}
	if factory.closeCalls != 1 {
		t.Fatalf("factory.closeCalls = %d, want 1", factory.closeCalls)
	}
}

func TestNewStorageReaderClosesFactoryOnNilReader(t *testing.T) {
	t.Parallel()

	// 修正ポイント：reader フィールドが初期値(nil)のままの状態。
	// これにより InputReader() が (remoteio.InputReader)(nil) を返すことをシミュレート。
	factory := &stubFactory{}

	_, _, err := newStorageReader(context.Background(), func(context.Context) (remoteio.IOFactory, error) {
		return factory, nil
	})
	if err == nil {
		t.Fatal("newStorageReader() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "reader is nil") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if factory.closeCalls != 1 {
		t.Fatalf("factory.closeCalls = %d, want 1", factory.closeCalls)
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

// TestOpenStorageInitializesSchemesIndependently は、片方のスキームの初期化が
// 滞っていても、もう片方が独立して開けることを確認します。初期化には認証情報の
// 解決などの I/O が伴うため、ここを共有ロックにすると一方の遅延が他方を巻き込みます。
func TestOpenStorageInitializesSchemesIndependently(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	gcsStarted := make(chan struct{})

	r := newTestReader(t, &stubExtractor{},
		WithGCSFactory(func(context.Context) (remoteio.IOFactory, error) {
			close(gcsStarted)
			<-release // GCS の初期化を意図的に滞留させる
			return &stubFactory{reader: &stubReader{content: "gcs body"}}, nil
		}),
		WithS3Factory(func(context.Context) (remoteio.IOFactory, error) {
			return &stubFactory{reader: &stubReader{content: "s3 body"}}, nil
		}),
	)

	// 失敗時もテストが停止しないよう、滞留を解除してから Close する。
	// t.Cleanup は LIFO なので、後から登録した解除処理が先に走る。
	t.Cleanup(func() { _ = r.Close() })
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	go func() {
		stream, err := r.Open(context.Background(), "gs://bucket/blocked.txt")
		if err == nil {
			_ = stream.Close()
		}
	}()

	<-gcsStarted

	type result struct {
		body string
		err  error
	}
	s3Done := make(chan result, 1)
	go func() {
		stream, err := r.Open(context.Background(), "s3://bucket/path.txt")
		if err != nil {
			s3Done <- result{err: err}
			return
		}
		defer stream.Close()
		body, err := io.ReadAll(stream)
		s3Done <- result{body: string(body), err: err}
	}()

	select {
	case got := <-s3Done:
		if got.err != nil {
			t.Fatalf("Open(s3) error = %v", got.err)
		}
		if got.body != "s3 body" {
			t.Fatalf("body = %q, want %q", got.body, "s3 body")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("S3 の Open が GCS の初期化完了を待たされている")
	}
}

func TestOpenStorageReusesCachedReader(t *testing.T) {
	t.Parallel()

	var factoryCalls int
	r := newTestReader(t, &stubExtractor{},
		WithGCSFactory(func(context.Context) (remoteio.IOFactory, error) {
			factoryCalls++
			return &stubFactory{reader: &stubReader{content: "gcs body"}}, nil
		}),
	)
	defer func() { _ = r.Close() }()

	for i := range 3 {
		stream, err := r.Open(context.Background(), "gs://bucket/path.txt")
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		if err := stream.Close(); err != nil {
			t.Fatalf("stream.Close() error = %v", err)
		}
	}

	if factoryCalls != 1 {
		t.Fatalf("factoryCalls = %d, want 1", factoryCalls)
	}
}

// Close 後の Open は接続を張り直さないこと。
//
// 以前は解放後の Open がファクトリを呼び直しており、Close したつもりの
// リーダーが新しい GCS クライアントを作っていました。io.Closer の慣習どおり
// Close を終端にして、この経路を塞いでいます。
func TestOpenAfterCloseDoesNotReinitialize(t *testing.T) {
	t.Parallel()

	var factories []*stubFactory
	r := newTestReader(t, &stubExtractor{},
		WithGCSFactory(func(context.Context) (remoteio.IOFactory, error) {
			f := &stubFactory{reader: &stubReader{content: "gcs body"}}
			factories = append(factories, f)
			return f, nil
		}),
	)

	stream, err := r.Open(context.Background(), "gs://bucket/path.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close() error = %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := r.Open(context.Background(), "gs://bucket/path.txt"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Open() after Close error = %v, want ErrClosed", err)
	}

	if len(factories) != 1 {
		t.Fatalf("factory count = %d, want 1 (Close 後に作り直してはいけない)", len(factories))
	}
	if factories[0].closeCalls != 1 {
		t.Fatalf("factories[0].closeCalls = %d, want 1", factories[0].closeCalls)
	}

	// Close は冪等であること（多重解放でエラーにしない）。
	if err := r.Close(); err != nil {
		t.Fatalf("2 度目の Close() error = %v", err)
	}
	if factories[0].closeCalls != 1 {
		t.Fatalf("2 度目の Close で closeCalls = %d, want 1", factories[0].closeCalls)
	}
}

func TestResolveMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		want        string
		wantErr     bool
	}{
		{name: "empty header", contentType: "", want: ""},
		{name: "with charset", contentType: "text/html; charset=utf-8", want: "text/html"},
		{name: "uppercase", contentType: "TEXT/HTML", want: "text/html"},
		{name: "no parameters", contentType: "image/png", want: "image/png"},
		{name: "malformed but known", contentType: `text/html; charset="`, want: "text/html"},
		{name: "malformed but known image", contentType: `image/jpeg; foo="`, want: "image/jpeg"},
		{name: "malformed and unknown", contentType: `text/html-sandboxed; charset="`, wantErr: true},
		{name: "malformed and unsupported", contentType: `application/json; charset="`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveMediaType(tt.contentType)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveMediaType(%q) error = nil, want error", tt.contentType)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveMediaType(%q) error = %v", tt.contentType, err)
			}
			if got != tt.want {
				t.Fatalf("resolveMediaType(%q) = %q, want %q", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestClassifyMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		want      mediaKind
	}{
		{mediaType: "text/html", want: mediaKindHTML},
		{mediaType: "application/xhtml+xml", want: mediaKindHTML},
		{mediaType: "text/plain", want: mediaKindPassthrough},
		{mediaType: "text/markdown", want: mediaKindPassthrough},
		{mediaType: "text/x-markdown", want: mediaKindPassthrough},
		{mediaType: "image/png", want: mediaKindPassthrough},
		{mediaType: "image/svg+xml", want: mediaKindPassthrough},
		{mediaType: "application/json", want: mediaKindUnsupported},
		{mediaType: "text/html-sandboxed", want: mediaKindUnsupported},
		{mediaType: "", want: mediaKindUnsupported},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			t.Parallel()

			if got := classifyMediaType(tt.mediaType); got != tt.want {
				t.Fatalf("classifyMediaType(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
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
