package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

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

func TestExecuteReadsAndTrimsContent(t *testing.T) {
	t.Parallel()

	reader := &stubContentReader{
		stream: io.NopCloser(strings.NewReader("  hello world\n")),
	}
	p := NewPipeline(" https://example.com/article ", reader)

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

func TestExecuteValidatesRequiredDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		p    *Pipeline
	}{
		{name: "nil pipeline", ctx: context.Background(), p: nil},
		{name: "nil context", ctx: nil, p: NewPipeline("https://example.com", &stubContentReader{})},
		{name: "empty source", ctx: context.Background(), p: NewPipeline(" ", &stubContentReader{})},
		{name: "nil reader", ctx: context.Background(), p: NewPipeline("https://example.com", nil)},
		{
			name: "nil stream",
			ctx:  context.Background(),
			p:    NewPipeline("https://example.com", &stubContentReader{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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
	p := NewPipeline("https://example.com", &stubContentReader{
		stream: errReadCloser{err: readErr},
	})

	_, err := p.Execute(context.Background())
	if !errors.Is(err, readErr) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, readErr)
	}
}

func TestExecuteWrapsOpenErrors(t *testing.T) {
	t.Parallel()

	openErr := errors.New("open failed")
	p := NewPipeline("https://example.com", &stubContentReader{err: openErr})

	_, err := p.Execute(context.Background())
	if !errors.Is(err, openErr) {
		t.Fatalf("Execute() error = %v, want wrapping %v", err, openErr)
	}
}
