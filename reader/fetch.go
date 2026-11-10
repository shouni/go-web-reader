package reader

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-http-kit/retry"
)

// fetched is the result of one fetch. retry.RunValue carries only a single value, so the body and the
// Content-Type are bundled into one.
type fetched struct {
	body        []byte
	contentType string
}

// fetchBytes GETs the URI and retries transient failures with exponential backoff.
//
// Retry lives here because the HTTPClient seam is only Do, and the decision to redo the same GET
// cannot be made from a single response. The default httpkit.Client does not retry Do either.
// It does not delegate to httpkit.Get, because that builds its own request and the headers from
// newHTTPRequest would be lost.
func (r *UniversalReader) fetchBytes(ctx context.Context, uri string) (fetched, error) {
	if r.retry.maxRetries == 0 {
		return r.fetchOnce(ctx, uri)
	}

	return retry.RunValue(ctx, func() (fetched, error) {
		return r.fetchOnce(ctx, uri)
	},
		retry.WithName("GET "+uri),
		retry.WithMaxRetries(r.retry.maxRetries),
		retry.WithInitialInterval(r.retry.initialInterval),
		retry.WithMaxInterval(r.retry.maxInterval),
		// Apply the same bound to Retry-After. Open is a synchronous API and the target is a URL the
		// user entered, so without a bound the remote server would decide how long we wait.
		retry.WithMaxRetryAfter(r.retry.maxInterval),
		retry.WithShouldRetry(r.shouldRetryFetch),
	)
}

// fetchOnce GETs once. A used *http.Request cannot be resent, so the request is rebuilt on every
// call.
func (r *UniversalReader) fetchOnce(ctx context.Context, uri string) (fetched, error) {
	req, err := newHTTPRequest(ctx, uri)
	if err != nil {
		return fetched{}, err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fetched{}, fmt.Errorf("HTTP request failed: %w", err)
	}

	// HandleResponse does the nil checks on resp and resp.Body, the Close and the size cap
	// (closing here would close twice). A swapped client may return (nil, nil), so the headers
	// are read only after it.
	body, err := httpkit.HandleResponse(resp)
	if err != nil {
		return fetched{}, err
	}

	return fetched{body: body, contentType: resp.Header.Get("Content-Type")}, nil
}

// newHTTPRequest builds the HTTP GET request shared by the reader.
func newHTTPRequest(ctx context.Context, uri string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("User-Agent", httpkit.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", httpkit.AcceptLanguage)
	req.Header.Set("sec-ch-ua", httpkit.SecChUA)
	req.Header.Set("sec-ch-ua-mobile", httpkit.SecChUAMobile)
	req.Header.Set("sec-ch-ua-platform", httpkit.SecChUAPlatform)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	return req, nil
}

// shouldRetryFetch reports whether a fetch failure is worth retrying.
func (r *UniversalReader) shouldRetryFetch(err error) bool {
	if err == nil {
		return false
	}
	// Leave the decision to a client that can make it (see RetryClassifier).
	if classifier, ok := r.httpClient.(RetryClassifier); ok {
		return classifier.IsHTTPRetryableError(err)
	}

	// Resuming an operation the caller cancelled would only lengthen the wait.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// 4xx and malformed requests or responses do not change on repetition.
	if httpkit.IsNonRetryableError(err) ||
		errors.Is(err, httpkit.ErrResponseBodyTooLarge) ||
		errors.Is(err, httpkit.ErrNilResponse) ||
		errors.Is(err, httpkit.ErrNilResponseBody) {
		return false
	}

	// Treat 5xx / 408 / 429 and unclassifiable network errors as transient.
	return true
}
