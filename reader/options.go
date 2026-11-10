package reader

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
	"github.com/shouni/go-web-reader/extract"
	"github.com/shouni/netarmor/securenet"
)

// Retry defaults. They are shorter than httpkit's defaults (5 s initial, 30 s max).
// Open is a synchronous API that keeps the caller waiting, so the total wait directly shapes how it feels.
const (
	// DefaultMaxRetries is the default number of times an HTTP fetch is retried (not counting the first attempt).
	DefaultMaxRetries = 2
	// DefaultRetryInitialInterval is the initial wait before a retry.
	DefaultRetryInitialInterval = 500 * time.Millisecond
	// DefaultRetryMaxInterval is the upper bound of the wait before a retry.
	DefaultRetryMaxInterval = 4 * time.Second
)

// Extractor extracts the main text from already-fetched HTML.
// The second return value reports whether a body was found.
type Extractor interface {
	Extract(ctx context.Context, r io.Reader) (text string, hasBody bool, err error)
}

// ContentTypeExtractor is an extractor that also accepts the Content-Type header.
//
// It is not a separate seam but an extra capability the extractor passed to WithExtractor may have.
// When it is satisfied, this is called instead of Extract.
//
// Charset conversion can happen in only one place, the extractor that can read <meta charset>
// (bytes already converted to UTF-8 would break if interpreted as Shift_JIS again), so
// reader only passes the Content-Type along as a hint and does not convert.
type ContentTypeExtractor interface {
	ExtractWithContentType(ctx context.Context, r io.Reader, contentType string) (text string, hasBody bool, err error)
}

// HTTPClient is the minimal interface for executing an HTTP request.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// RetryClassifier is an HTTP client that can decide whether a fetch failure should be retried.
// When the HTTPClient satisfies it, its decision takes precedence. The client that returned an error is
// the one that knows its type, so reader does not keep a second definition of what is retryable.
type RetryClassifier interface {
	IsHTTPRetryableError(err error) bool
}

// SafeURLValidator checks that a URI is safe, returning nil when it is.
// It takes a context because it resolves names.
type SafeURLValidator func(context.Context, string) error

// StorageFactory creates the set of GCS/S3 clients.
// It establishes real connections, so it is called only on the first Open for its scheme.
type StorageFactory func(context.Context) (remoteio.Factory, error)

// Option swaps a dependency of UniversalReader.
//
// A nil Option, and a With* given a nil value, are ignored and the default is kept.
// The reader then runs with the default while you believe it was swapped, so callers should make sure
// the value they pass is not nil.
type Option func(*options)

// retryPolicy is the retry configuration for HTTP fetches.
// When maxRetries is 0 the fetch runs once and is not retried.
type retryPolicy struct {
	maxRetries      uint
	initialInterval time.Duration
	maxInterval     time.Duration
}

type options struct {
	extractor     Extractor
	httpClient    HTTPClient
	safeURL       SafeURLValidator
	newGCSFactory StorageFactory
	newS3Factory  StorageFactory
	retry         retryPolicy
}

// newOptions returns the defaults with the options applied.
// Every With* ignores nil, so no field can end up nil, and New does not check again.
func newOptions(opts ...Option) options {
	cfg := options{
		// securenet.ValidateURL takes variadic options, so it cannot be assigned directly.
		safeURL: func(ctx context.Context, uri string) error {
			return securenet.ValidateURL(ctx, uri)
		},
		extractor:     extract.Engine{},
		httpClient:    httpkit.New(),
		newGCSFactory: func(ctx context.Context) (remoteio.Factory, error) { return gcs.New(ctx) },
		newS3Factory:  func(ctx context.Context) (remoteio.Factory, error) { return s3.New(ctx) },
		retry: retryPolicy{
			maxRetries:      DefaultMaxRetries,
			initialInterval: DefaultRetryInitialInterval,
			maxInterval:     DefaultRetryMaxInterval,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// WithMaxRetries sets how many times an HTTP fetch is retried (not counting the first attempt).
// Passing 0 disables retries.
//
// Only 5xx / 408 / 429 and network errors that cannot be classified (timeouts and the like) are
// retried. Other 4xx responses and an oversized response are not, because repeating the same request
// would not change the result.
//
// When the response has Retry-After, the next wait is the instructed value rather than the one
// computed by exponential backoff, because resending before the time the server asked for would only
// get the same refusal. If the instructed value exceeds WithRetryInterval's upper bound, however, it
// gives up without waiting and returns retry.ErrRetryAfterTooLong. The target is a URL the user
// entered, and without a bound the other side would decide how long we wait.
//
// When swapping the default client for your own retrying client with WithHTTPClient, pass 0 so the
// waits are not doubled.
func WithMaxRetries(n uint) Option {
	return func(o *options) {
		o.retry.maxRetries = n
	}
}

// WithRetryInterval sets the wait before a retry (the initial value and upper bound of the exponential
// backoff). Values of 0 or less are ignored and the defaults are kept. The upper bound also applies to
// Retry-After instructions (see WithMaxRetries).
func WithRetryInterval(initialInterval, maxInterval time.Duration) Option {
	return func(o *options) {
		if initialInterval > 0 {
			o.retry.initialInterval = initialInterval
		}
		if maxInterval > 0 {
			o.retry.maxInterval = maxInterval
		}
	}
}

// WithExtractor swaps the text extractor.
// HTTP fetching itself is not swapped (that is WithHTTPClient).
//
// When the extractor also satisfies ContentTypeExtractor, it is called with the Content-Type header
// (the default extract.Engine does).
func WithExtractor(extractor Extractor) Option {
	return func(o *options) {
		if extractor != nil {
			o.extractor = extractor
		}
	}
}

// WithHTTPClient swaps the client used for HTTP(S) fetches.
//
// This is also the place to change request headers: rewrite the *http.Request inside Do, then
// delegate to the original client.
//
// The response size cap is not removed, because it is applied outside the client (by go-http-kit's
// handling that reads the returned *http.Response to the end).
func WithHTTPClient(client HTTPClient) Option {
	return func(o *options) {
		if client != nil {
			o.httpClient = client
		}
	}
}

// WithSafeURLValidator swaps the URL safety check.
//
// The validator is called only on the HTTP(S) branch. gs:// / s3:// do not go through it, because
// the cloud SDK decides where to connect, so it is not a seam for rejecting storage URIs.
//
// To point at a local test server, swap WithHTTPClient as well. Loosening only the validator still
// fails at the IP check the default client performs right before connecting.
func WithSafeURLValidator(fn SafeURLValidator) Option {
	return func(o *options) {
		if fn != nil {
			o.safeURL = fn
		}
	}
}

// WithGCSFactory swaps how the GCS factory is created.
func WithGCSFactory(fn StorageFactory) Option {
	return func(o *options) {
		if fn != nil {
			o.newGCSFactory = fn
		}
	}
}

// WithS3Factory swaps how the S3 factory is created.
func WithS3Factory(fn StorageFactory) Option {
	return func(o *options) {
		if fn != nil {
			o.newS3Factory = fn
		}
	}
}
