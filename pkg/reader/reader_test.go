package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-web-exact/v2/ports"
)

// --- Stubs ---

type stubExtractor struct {
	text          string
	hasBody       bool
	err           error
	lastURL       string
	extractedBody string
	fetchCalls    int
	extractCalls  int
}

func (s *stubExtractor) FetchAndExtractText(_ context.Context, url string) (string, bool, error) {
	s.lastURL = url
	s.fetchCalls++
	return s.text, s.hasBody, s.err
}

func (s *stubExtractor) ExtractText(_ context.Context, reader io.Reader) (string, bool, error) {
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
func (s *stubReader) List(_ context.Context, _ string, _ func(string) error) error { return nil }
func (s *stubReader) Exists(_ context.Context, _ string) (bool, error)             { return true, nil }

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
	if extractor.fetchCalls != 0 {
		t.Fatalf("extractor.fetchCalls = %d, want 0", extractor.fetchCalls)
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

	r, err := New(
		WithHTTPClient(&stubHTTPClient{}),
		WithSafeURLValidator(func(string) (bool, error) { return true, nil }),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if r == nil {
		t.Fatal("New() reader = nil")
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
	if extractor.extractCalls != 0 || extractor.fetchCalls != 0 {
		t.Fatalf("extractor calls = extract:%d fetch:%d, want 0", extractor.extractCalls, extractor.fetchCalls)
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
	if extractor.extractCalls != 0 || extractor.fetchCalls != 0 {
		t.Fatalf("extractor calls = extract:%d fetch:%d, want 0", extractor.extractCalls, extractor.fetchCalls)
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
	if extractor.extractCalls != 0 || extractor.fetchCalls != 0 {
		t.Fatalf("extractor calls = extract:%d fetch:%d, want 0", extractor.extractCalls, extractor.fetchCalls)
	}
}

func TestReadGCSUsesInjectedReader(t *testing.T) {
	t.Parallel()

	storageReader := &stubReader{content: "gcs body"}
	r := newTestReader(t, &stubExtractor{})
	r.gcs.reader = storageReader

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
	r.s3.reader = storageReader

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
		{name: "unsafe uri", ctx: context.Background(), uri: "https://example.com/private", opts: []Option{WithSafeURLValidator(func(string) (bool, error) { return false, nil })}},
		{name: "safe checker error", ctx: context.Background(), uri: "https://example.com/private", opts: []Option{WithSafeURLValidator(func(string) (bool, error) { return false, safeCheckErr })}},
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
	r := newTestReader(t, &stubExtractor{}, WithSafeURLValidator(func(string) (bool, error) {
		return false, safeCheckErr
	}))

	_, err := r.Open(context.Background(), "https://example.com/private")
	if !errors.Is(err, safeCheckErr) {
		t.Fatalf("Open() error = %v, want wrapping %v", err, safeCheckErr)
	}
}

func TestNewRejectsNilRequiredDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  Option
	}{
		{name: "safe URL validator", opt: WithSafeURLValidator(nil)},
		{name: "GCS factory", opt: WithGCSFactory(nil)},
		{name: "S3 factory", opt: WithS3Factory(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := New(WithExtractor(&stubExtractor{}), tt.opt)
			if err == nil {
				t.Fatal("New() error = nil, want error")
			}
		})
	}
}

func TestCloseClosesManagedResources(t *testing.T) {
	t.Parallel()

	gcsCloser := &stubCloser{}
	s3Closer := &stubCloser{}
	r := newTestReader(t, &stubExtractor{})
	r.gcs.reader = &stubReader{}
	r.gcs.closer = gcsCloser
	r.s3.reader = &stubReader{}
	r.s3.closer = s3Closer

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if gcsCloser.closed != 1 || s3Closer.closed != 1 {
		t.Fatalf("close counts = (%d, %d), want (1, 1)", gcsCloser.closed, s3Closer.closed)
	}
	if r.gcs.reader != nil || r.gcs.closer != nil || r.s3.reader != nil || r.s3.closer != nil {
		t.Fatal("managed resources were not cleared")
	}
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

func newTestReader(t *testing.T, extractor ports.Extractor, opts ...Option) *UniversalReader {
	t.Helper()

	baseOpts := []Option{
		WithExtractor(extractor),
		WithSafeURLValidator(func(string) (bool, error) { return true, nil }),
	}
	baseOpts = append(baseOpts, opts...)

	reader, err := New(baseOpts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return reader
}
