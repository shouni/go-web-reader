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

// Assertions that catch a stub falling behind a dependency's interface change at build time rather
// than at test time (so that a change like go-remote-io v1.7.0 adding ListOption to List is not missed).
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

// headerOverridingClient is a client that overrides the default headers before delegating.
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
		t.Fatal("the default extractor is not set")
	}
}

// Options given nil must be ignored, keeping the defaults.
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
				t.Fatal("a nil option lost a default dependency")
			}
			for _, scheme := range []string{remoteio.SchemeGCS, remoteio.SchemeS3} {
				if storageCache(t, r, scheme).newFactory == nil {
					t.Fatalf("the %s factory was lost", scheme)
				}
			}
		})
	}
}

// Request headers must be swappable through WithHTTPClient.
// There is no seam that replaces the whole fetch, so callers who want other headers rely on this path.
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
	if !strings.Contains(err.Error(), "unsupported URI scheme") {
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

// The URL safety check must apply only to HTTP(S).
// For gs:// / s3://, where the cloud SDK decides where to connect, there is no peer to check.
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
				t.Fatalf("validator called = %v (%v), want %v", called, validated, tt.wantCalled)
			}
		})
	}
}

// Close must be terminal even for HTTP, which has nothing to release.
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
		t.Fatalf("client.calls = %d, want 0 (must not fetch after Close)", client.calls)
	}
}

// ReadAll must open, read to the end and close.
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

// ReadAll has no limit, and gs:// / s3:// do not even get HTTP's 25MB. Each service was writing a
// bounded read itself, so this pins that routine (read one byte more to tell an overflow).
func TestReadAllLimit(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{}, WithHTTPClient(&stubHTTPClient{
		contentType: "text/plain",
		body:        "0123456789",
	}))

	if body, err := r.ReadAllLimit(context.Background(), "https://example.com/a.txt", 10); err != nil || string(body) != "0123456789" {
		t.Errorf("ReadAllLimit(exact) = %q, %v; want the whole body, nil", body, err)
	}
	if _, err := r.ReadAllLimit(context.Background(), "https://example.com/a.txt", 9); !errors.Is(err, ErrTooLarge) {
		t.Errorf("ReadAllLimit(9) error = %v, want ErrTooLarge", err)
	}
	if _, err := r.ReadAllLimit(context.Background(), "https://example.com/a.txt", 0); err == nil {
		t.Error("ReadAllLimit(0) error = nil, want error")
	}
}

// ReadText truncates at the limit and drops a tail cut inside a multi-byte character.
func TestReadTextTruncatesAtRuneBoundary(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{}, WithHTTPClient(&stubHTTPClient{
		contentType: "text/plain",
		body:        "あいう", // 9 bytes
	}))

	text, truncated, err := r.ReadText(context.Background(), "https://example.com/a.txt", 4)
	if err != nil {
		t.Fatalf("ReadText() error = %v", err)
	}
	if !truncated || text != "あ" {
		t.Errorf("ReadText(4) = %q, %v; want \"あ\", true", text, truncated)
	}

	text, truncated, err = r.ReadText(context.Background(), "https://example.com/a.txt", 9)
	if err != nil || truncated || text != "あいう" {
		t.Errorf("ReadText(9) = %q, %v, %v; want the whole body, false, nil", text, truncated, err)
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
