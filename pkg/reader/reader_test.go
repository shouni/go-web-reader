package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
)

type stubExtractor struct {
	text    string
	hasBody bool
	err     error
	lastURL string
}

func (s *stubExtractor) FetchAndExtractText(_ context.Context, url string) (string, bool, error) {
	s.lastURL = url
	return s.text, s.hasBody, s.err
}

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

type stubCloser struct {
	closed int
	err    error
}

func (s *stubCloser) Close() error {
	s.closed++
	return s.err
}

type stubFactory struct {
	reader     remoteio.Reader
	readerErr  error
	closeErr   error
	closeCalls int
}

func (s *stubFactory) Reader() (remoteio.Reader, error) {
	if s.readerErr != nil {
		return nil, s.readerErr
	}
	return s.reader, nil
}

func (s *stubFactory) Writer() (remoteio.Writer, error) {
	return nil, nil
}

func (s *stubFactory) Close() error {
	s.closeCalls++
	return s.closeErr
}

func TestReadHTTPUsesExtractor(t *testing.T) {
	t.Parallel()
	stubSafeURL(t, func(string) (bool, error) { return true, nil })

	extractor := &stubExtractor{text: "hello world", hasBody: true}
	r := &UniversalReader{extractor: extractor}

	stream, err := r.Read(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "hello world" {
		t.Fatalf("body = %q, want %q", got, "hello world")
	}
	if extractor.lastURL != "https://example.com/article" {
		t.Fatalf("extractor.lastURL = %q", extractor.lastURL)
	}
}

func TestReadHTTPNoBodyReturnsError(t *testing.T) {
	t.Parallel()
	stubSafeURL(t, func(string) (bool, error) { return true, nil })

	r := &UniversalReader{
		extractor: &stubExtractor{hasBody: false},
	}

	_, err := r.Read(context.Background(), "https://example.com/empty")
	if err == nil {
		t.Fatal("Read() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "コンテンツが見つかりませんでした") {
		t.Fatalf("Read() error = %v", err)
	}
}

func TestReadGCSUsesInjectedReader(t *testing.T) {
	t.Parallel()
	stubSafeURL(t, func(string) (bool, error) { return true, nil })

	storageReader := &stubReader{content: "gcs body"}
	r := &UniversalReader{
		extractor: &stubExtractor{},
		gcsReader: storageReader,
	}

	stream, err := r.Read(context.Background(), "gs://bucket/path.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
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
	stubSafeURL(t, func(string) (bool, error) { return true, nil })

	storageReader := &stubReader{content: "s3 body"}
	r := &UniversalReader{
		extractor: &stubExtractor{},
		s3Reader:  storageReader,
	}

	stream, err := r.Read(context.Background(), "s3://bucket/path.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
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

	tests := []struct {
		name string
		ctx  context.Context
		uri  string
		safe func(string) (bool, error)
	}{
		{name: "nil context", ctx: nil, uri: "https://example.com", safe: func(string) (bool, error) { return true, nil }},
		{name: "empty uri", ctx: context.Background(), uri: "", safe: func(string) (bool, error) { return true, nil }},
		{name: "unsafe uri", ctx: context.Background(), uri: "https://example.com/private", safe: func(string) (bool, error) { return false, nil }},
		{name: "safe checker error", ctx: context.Background(), uri: "https://example.com/private", safe: func(string) (bool, error) { return false, fmt.Errorf("lookup failed") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stubSafeURL(t, tt.safe)

			r := &UniversalReader{extractor: &stubExtractor{}}

			_, err := r.Read(tt.ctx, tt.uri)
			if err == nil {
				t.Fatal("Read() error = nil, want error")
			}
		})
	}
}

func TestCloseClosesManagedResources(t *testing.T) {
	t.Parallel()

	gcsCloser := &stubCloser{}
	s3Closer := &stubCloser{}
	r := &UniversalReader{
		gcsReader: &stubReader{},
		gcsCloser: gcsCloser,
		s3Reader:  &stubReader{},
		s3Closer:  s3Closer,
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if gcsCloser.closed != 1 || s3Closer.closed != 1 {
		t.Fatalf("close counts = (%d, %d), want (1, 1)", gcsCloser.closed, s3Closer.closed)
	}
	if r.gcsReader != nil || r.gcsCloser != nil || r.s3Reader != nil || r.s3Closer != nil {
		t.Fatal("managed resources were not cleared")
	}
}

func TestNewStorageReaderClosesFactoryOnReaderError(t *testing.T) {
	t.Parallel()

	factory := &stubFactory{
		readerErr: errors.New("reader failed"),
	}

	_, _, err := newStorageReader(context.Background(), func(context.Context) (remoteio.ReadWriteFactory, error) {
		return factory, nil
	})
	if err == nil {
		t.Fatal("newStorageReader() error = nil, want error")
	}
	if factory.closeCalls != 1 {
		t.Fatalf("factory.closeCalls = %d, want 1", factory.closeCalls)
	}
}

func stubSafeURL(t *testing.T, fn func(string) (bool, error)) {
	t.Helper()

	orig := isSafeURL
	isSafeURL = fn
	t.Cleanup(func() {
		isSafeURL = orig
	})
}
