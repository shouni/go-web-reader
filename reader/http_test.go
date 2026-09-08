package reader

import (
	"context"
	"io"
	"strings"
	"testing"
)

var _ ContentTypeExtractor = (*contentTypeExtractorStub)(nil)

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

func TestOpenHTTPUsesExtractor(t *testing.T) {
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

func TestOpenHTTPPlainTextReturnsBodyWithoutExtractor(t *testing.T) {
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

func TestOpenHTTPMarkdownReturnsBodyWithoutExtractor(t *testing.T) {
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

func TestOpenHTTPImageReturnsBodyWithoutExtractor(t *testing.T) {
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

func TestOpenHTTPUnsupportedContentTypeReturnsError(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{text: "html text", hasBody: true}
	r := newTestReader(t, extractor, WithHTTPClient(&stubHTTPClient{
		contentType: "application/octet-stream",
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

func TestOpenHTTPFallsBackForMalformedContentType(t *testing.T) {
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

func TestOpenHTTPMalformedContentTypeDoesNotFallbackOnPartialMatch(t *testing.T) {
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

func TestOpenHTTPNoBodyReturnsError(t *testing.T) {
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
		{name: "malformed and unsupported", contentType: `application/octet-stream; charset="`, wantErr: true},
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
		{mediaType: "text/csv", want: mediaKindPassthrough},
		{mediaType: "application/json", want: mediaKindPassthrough},
		{mediaType: "application/xml", want: mediaKindPassthrough},
		{mediaType: "text/xml", want: mediaKindPassthrough},
		{mediaType: "application/octet-stream", want: mediaKindUnsupported},
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
