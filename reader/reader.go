// Package reader provides a universal reader that reads content from any kind of URI, such as
// HTTP/HTTPS or GCS/S3, and extracts the content where needed.
package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"unicode/utf8"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/netarmor/securenet"
)

// The HTTP(S) scheme names. They have the same granularity as remoteio.SchemeGCS / SchemeS3, so all of
// Open's dispatch is a string comparison of this form.
//
// The spelling comes from netarmor's constants. If the spelling securenet considers "http" and the one
// dispatched here as HTTP drifted apart, a URI that passed validation would fall into another branch.
const (
	schemeHTTP  = securenet.SchemeHTTP
	schemeHTTPS = securenet.SchemeHTTPS
)

// ErrClosed reports an attempt to use a UniversalReader that has been closed.
var ErrClosed = errors.New("reader is closed")

// ErrTooLarge reports that the content exceeded the limit in ReadAllLimit.
var ErrTooLarge = errors.New("reader: content exceeds the size limit")

// UniversalReader returns a read stream according to the kind of URI.
type UniversalReader struct {
	extractor  Extractor
	httpClient HTTPClient
	safeURL    SafeURLValidator
	retry      retryPolicy
	// storages holds the storage readers keyed by scheme name. They are not separate fields, so that
	// adding a scheme touches only the map built in New.
	storages map[string]*storageReaderCache
	// closed marks the reader as closed. Each per-scheme cache has the same mark, but HTTP, which has
	// nothing to release, has none, so with those alone https:// would still read after Close.
	closed atomic.Bool
}

// Open determines the URI's scheme and returns the appropriate read stream.
//
// It handles four schemes, https:// / http:// / gs:// / s3://, and returns an
// "unsupported URI scheme" error for anything else. Local file paths are out of scope.
//
// The scheme must be written in lower case. The check is a plain string comparison of what precedes
// "://"; unlike url.Parse it does not normalise case, so an upper-case scheme such as HTTPS:// is
// treated as unsupported.
//
// gs:// / s3:// return a stream, but HTTP(S) reads the whole response before returning, for
// main-content extraction and retries. Reading the returned stream bit by bit saves no memory
// (the cap is go-http-kit's response size limit).
func (r *UniversalReader) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if uri == "" {
		return nil, fmt.Errorf("uri cannot be empty")
	}
	if r.closed.Load() {
		return nil, fmt.Errorf("reader is not available: %w", ErrClosed)
	}

	// The scheme is taken with the same function as go-remote-io. gs/s3 URIs are handed to remoteio as
	// they are, so disagreeing on where the scheme ends would be a real bug.
	scheme := remoteio.Scheme(uri)

	// Dispatch comes before the URL safety check, and the check applies only to HTTP(S). The check asks
	// whether the peer we dial ourselves is safe, and gs:// / s3://, where the cloud SDK decides where to
	// connect, have no such peer. Moreover securenet.ValidateURL rejects anything other than http/https
	// as a scheme violation, so running every scheme through it would make GCS/S3 impossible to open.
	//
	// Do not use securenet's checks (IsSecureServiceURL / ValidateURL) as the dispatch condition.
	// The former allows plain HTTP only to localhost and the like, which would leave http:// unsupported
	// altogether, and the latter would let a caller-swapped validator decide which backend handles a URI.
	if scheme == schemeHTTP || scheme == schemeHTTPS {
		if err := r.safeURL(ctx, uri); err != nil {
			return nil, fmt.Errorf("URL safety check failed: %w", err)
		}
		return r.openHTTP(ctx, uri)
	}
	if cache, ok := r.storages[scheme]; ok {
		return r.openStorage(ctx, uri, cache)
	}

	return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
}

// ReadAll reads the URI's content to the end and returns it.
// It folds opening, reading to the end and closing into one call, so the stream cannot be left open.
func (r *UniversalReader) ReadAll(ctx context.Context, uri string) ([]byte, error) {
	stream, err := r.Open(ctx, uri)
	if err != nil {
		return nil, err
	}

	// Keep both failures, reading and closing.
	data, readErr := io.ReadAll(stream)
	if err := errors.Join(readErr, stream.Close()); err != nil {
		return nil, fmt.Errorf("failed to read URI (%s): %w", uri, err)
	}

	return data, nil
}

// ReadAllLimit is like ReadAll, but returns ErrTooLarge when the content exceeds maxBytes.
//
// ReadAll has no limit. HTTP stops at HandleResponse's 25MB, but gs:// and s3:// hand the stream
// straight through, so applying a limit had become the caller's job. Each service was writing the same
// "read one byte more than the limit to tell an overflow" routine, so it lives here. To truncate
// instead, use ReadText.
func (r *UniversalReader) ReadAllLimit(ctx context.Context, uri string, maxBytes int64) ([]byte, error) {
	data, truncated, err := r.readLimited(ctx, uri, maxBytes)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, fmt.Errorf("%w: %s (limit: %d bytes)", ErrTooLarge, uri, maxBytes)
	}
	return data, nil
}

// ReadText reads up to maxBytes of the URI's content as a string and, when the content exceeds the
// limit, truncates it at a UTF-8 character boundary. The second return value reports whether it was
// truncated.
//
// It serves uses where dropping the excess is fine, such as body text put into a prompt. The caller
// should record or report the truncation; it is done silently here.
func (r *UniversalReader) ReadText(ctx context.Context, uri string, maxBytes int64) (string, bool, error) {
	data, truncated, err := r.readLimited(ctx, uri, maxBytes)
	if err != nil {
		return "", false, err
	}
	if truncated {
		// The end cut at the limit may fall inside a multi-byte character. Drop the incomplete bytes.
		for len(data) > 0 {
			if r, size := utf8.DecodeLastRune(data); r == utf8.RuneError && size == 1 {
				data = data[:len(data)-1]
				continue
			}
			break
		}
	}
	return string(data), truncated, nil
}

// readLimited reads one byte more than the limit to tell whether it was exceeded.
func (r *UniversalReader) readLimited(ctx context.Context, uri string, maxBytes int64) (data []byte, truncated bool, err error) {
	if maxBytes <= 0 {
		return nil, false, fmt.Errorf("maxBytes must be positive: %d", maxBytes)
	}
	stream, err := r.Open(ctx, uri)
	if err != nil {
		return nil, false, err
	}

	data, readErr := io.ReadAll(io.LimitReader(stream, maxBytes+1))
	if err := errors.Join(readErr, stream.Close()); err != nil {
		return nil, false, fmt.Errorf("failed to read URI (%s): %w", uri, err)
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

// Close releases the external resources held internally.
// Close is terminal: after it, Open returns ErrClosed for every scheme.
func (r *UniversalReader) Close() error {
	if r == nil {
		return nil
	}
	r.closed.Store(true)

	// Close everything rather than stopping at the first failure, since anything left unclosed keeps its connection.
	var errs []error
	for _, cache := range r.storages {
		if err := cache.close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// New creates a new UniversalReader.
//
// It returns no error because it establishes no external connection. GCS/S3 clients are not created
// until the first Open for that scheme, and that is where a failure would surface.
func New(opts ...Option) *UniversalReader {
	cfg := newOptions(opts...)

	return &UniversalReader{
		extractor:  cfg.extractor,
		httpClient: cfg.httpClient,
		safeURL:    cfg.safeURL,
		retry:      cfg.retry,
		storages: map[string]*storageReaderCache{
			remoteio.SchemeGCS: {label: "GCS", newFactory: cfg.newGCSFactory},
			remoteio.SchemeS3:  {label: "S3", newFactory: cfg.newS3Factory},
		},
	}
}
