package app

import (
	"errors"
	"io"
	"testing"
)

type stubCloser struct {
	closed int
	err    error
}

func (s *stubCloser) Close() error {
	s.closed++
	return s.err
}

func TestContainerCloseClosesAllResources(t *testing.T) {
	t.Parallel()

	firstErr := errors.New("first close failed")
	secondErr := errors.New("second close failed")
	first := &stubCloser{err: firstErr}
	second := &stubCloser{err: secondErr}
	c := &Container{
		Closers: []io.Closer{
			first,
			nil,
			second,
		},
	}

	err := c.Close()
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("Close() error = %v, want both close errors", err)
	}
	if first.closed != 1 || second.closed != 1 {
		t.Fatalf("close counts = (%d, %d), want (1, 1)", first.closed, second.closed)
	}
}

func TestNilContainerCloseIsNoop(t *testing.T) {
	t.Parallel()

	var c *Container
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}
