package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-http-kit/retry"
)

var (
	_ HTTPClient      = (*scriptedHTTPClient)(nil)
	_ RetryClassifier = (*neverRetryClient)(nil)
)

// response is what scriptedHTTPClient returns from one Do.
type response struct {
	statusCode  int
	contentType string
	retryAfter  string
	body        string
	err         error
}

// scriptedHTTPClient returns the next response on every call.
// Once the script runs out it repeats the last response, so it serves both for counting on which
// attempt a fetch succeeded and for checking the retry limit.
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
	if res.retryAfter != "" {
		resp.Header.Set("Retry-After", res.retryAfter)
	}
	return resp, nil
}

// neverRetryClient is a client that judges its own errors not worth retrying.
type neverRetryClient struct {
	scriptedHTTPClient
}

func (c *neverRetryClient) IsHTTPRetryableError(error) bool { return false }

// newRetryingTestReader returns a reader with waits shortened for tests.
func newRetryingTestReader(t *testing.T, client HTTPClient, opts ...Option) *UniversalReader {
	t.Helper()

	baseOpts := []Option{
		WithHTTPClient(client),
		WithRetryInterval(time.Millisecond, 2*time.Millisecond),
	}

	return newTestReader(t, &stubExtractor{text: "extracted", hasBody: true}, append(baseOpts, opts...)...)
}

// Transient failures (5xx / network errors) must be retried.
// The default client is built with retries, but the path that calls Do directly gets none, so
// retrying the fetch is the reader's responsibility.
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

// Failures that would not change on repetition must not be retried.
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

// WithMaxRetries(0) must stop retries.
// It is the seam for callers who inject their own retrying client, so they do not wait twice.
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

// The retry limit must not be exceeded (the first attempt + WithMaxRetries).
func TestFetchStopsAtMaxRetries(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{{statusCode: http.StatusServiceUnavailable, body: "unavailable"}}}
	r := newRetryingTestReader(t, client, WithMaxRetries(3))

	if _, err := r.Open(context.Background(), "https://example.com/flaky"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if client.calls != 4 {
		t.Fatalf("client.calls = %d, want 4 (first attempt + 3)", client.calls)
	}
}

// A Retry-After beyond the wait bound must not be honoured; give up without waiting.
// The target is a URL the user entered, so without a bound the other side would decide how long we wait.
func TestFetchStopsOnRetryAfterBeyondMaxInterval(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{
		{statusCode: http.StatusTooManyRequests, retryAfter: "3600", body: "slow down"},
	}}
	r := newRetryingTestReader(t, client, WithMaxRetries(3), WithRetryInterval(time.Millisecond, 10*time.Millisecond))

	start := time.Now()
	_, err := r.Open(context.Background(), "https://example.com/throttled")
	if !errors.Is(err, retry.ErrRetryAfterTooLong) {
		t.Fatalf("Open() error = %v, want retry.ErrRetryAfterTooLong", err)
	}
	if client.calls != 1 {
		t.Errorf("client.calls = %d, want 1", client.calls)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("waited %v where it should have given up", waited)
	}
}

// A Retry-After within the bound must be honoured.
func TestFetchHonorsRetryAfterWithinMaxInterval(t *testing.T) {
	t.Parallel()

	client := &scriptedHTTPClient{script: []response{
		{statusCode: http.StatusTooManyRequests, retryAfter: "1", body: "slow down"},
		{statusCode: http.StatusOK, contentType: "text/plain", body: "ok"},
	}}
	r := newRetryingTestReader(t, client, WithMaxRetries(3), WithRetryInterval(time.Millisecond, 2*time.Second))

	if _, err := r.Open(context.Background(), "https://example.com/throttled"); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if client.calls != 2 {
		t.Errorf("client.calls = %d, want 2", client.calls)
	}
}

// nilResponseClient is an HTTPClient that breaks the contract by returning (nil, nil).
type nilResponseClient struct{ calls int }

func (c *nilResponseClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, nil
}

// A swapped client returning (nil, nil) must not panic, and must return ErrNilResponse without
// retrying. This pins the wiring that runs HandleResponse's nil check before the headers are read.
func TestFetchRejectsNilResponseWithoutPanic(t *testing.T) {
	t.Parallel()

	client := &nilResponseClient{}
	r := newRetryingTestReader(t, client, WithMaxRetries(3))

	_, err := r.Open(context.Background(), "https://example.com/")
	if !errors.Is(err, httpkit.ErrNilResponse) {
		t.Fatalf("Open() error = %v, want httpkit.ErrNilResponse", err)
	}
	if client.calls != 1 {
		t.Errorf("client.calls = %d, want 1 (malformed responses are not retried)", client.calls)
	}
}

// When the client itself can decide whether to retry, its decision must be followed.
// The client that returned an error is the one that knows its type.
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
