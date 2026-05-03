package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// --- Stubs ---

type stubContentReader struct {
	stream  io.ReadCloser
	err     error
	lastURI string
}

func (s *stubContentReader) Open(_ context.Context, uri string) (io.ReadCloser, error) {
	s.lastURI = uri
	if s.err != nil {
		return nil, s.err
	}
	return s.stream, nil
}

type errReadCloser struct {
	err error
}

func (e errReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e errReadCloser) Close() error {
	return nil
}

// --- Tests ---

func TestNewPipeline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sourceURL string
		reader    ContentReader
		wantErr   bool
	}{
		{
			name:      "valid parameters",
			sourceURL: "https://example.com",
			reader:    &stubContentReader{},
			wantErr:   false,
		},
		{
			name:      "empty source URL",
			sourceURL: "",
			reader:    &stubContentReader{},
			wantErr:   true,
		},
		{
			name:      "nil reader",
			sourceURL: "https://example.com",
			reader:    nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPipeline(tt.sourceURL, tt.reader)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPipeline() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExecuteReadsAndTrimsContent(t *testing.T) {
	t.Parallel()

	reader := &stubContentReader{
		stream: io.NopCloser(strings.NewReader("  hello world\n")),
	}
	// Note: トリムは config 層で行われる前提なので、テストでもトリム済みの値を渡す
	p, err := NewPipeline("https://example.com/article", reader)
	if err != nil {
		t.Fatalf("NewPipeline() unexpected error = %v", err)
	}

	got, err := p.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "hello world" {
		t.Fatalf("Execute() = %q, want %q", got, "hello world")
	}
	if reader.lastURI != "https://example.com/article" {
		t.Fatalf("reader.lastURI = %q", reader.lastURI)
	}
}

func TestExecuteValidatesContextAndReceiver(t *testing.T) {
	t.Parallel()

	p, _ := NewPipeline("https://example.com", &stubContentReader{})

	tests := []struct {
		name string
		ctx  context.Context
		p    *Pipeline
	}{
		{name: "nil pipeline", ctx: context.Background(), p: nil},
		{name: "nil context", ctx: nil, p: p},
		{
			name: "nil stream from reader",
			ctx:  context.Background(),
			p:    p, // このテストケースは Execute 内の stream == nil チェックを通る
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// readerがnilを返すように設定（nil stream caseのため）
			if tt.name == "nil stream from reader" {
				p.reader = &stubContentReader{stream: nil}
			}

			_, err := tt.p.Execute(tt.ctx)
			if err == nil {
				t.Fatal("Execute() error = nil, want error")
			}
		})
	}
}

func TestExecuteWrapsReaderErrors(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	reader := &stubContentReader{
		stream: errReadCloser{err: readErr},
	}
	p, err := NewPipeline("https://example.com", reader)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	_, err = p.Execute(context.Background())
	if !errors.Is(err, readErr) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, readErr)
	}
}

func TestExecuteWrapsOpenErrors(t *testing.T) {
	t.Parallel()

	openErr := errors.New("open failed")
	p, err := NewPipeline("https://example.com", &stubContentReader{err: openErr})
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	_, err = p.Execute(context.Background())
	if !errors.Is(err, openErr) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, openErr)
	}
}
