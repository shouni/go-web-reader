package reader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"strings"
)

// mediaKind is how a body is handled, as decided by its media type.
type mediaKind int

const (
	// mediaKindUnsupported is a media type this reader does not handle.
	mediaKindUnsupported mediaKind = iota
	// mediaKindHTML is a media type run through the main-content extraction engine.
	mediaKindHTML
	// mediaKindPassthrough is a media type returned as is, without conversion.
	mediaKindPassthrough
)

// mediaKinds maps known media types to how they are handled. To support another Content-Type,
// update only this table (it drives both the dispatch and the rescue of broken headers).
var mediaKinds = map[string]mediaKind{
	"text/html":             mediaKindHTML,
	"application/xhtml+xml": mediaKindHTML,
	"text/plain":            mediaKindPassthrough,
	"text/markdown":         mediaKindPassthrough,
	"text/x-markdown":       mediaKindPassthrough,
	"text/csv":              mediaKindPassthrough,
	"application/json":      mediaKindPassthrough,
	"application/xml":       mediaKindPassthrough,
	"text/xml":              mediaKindPassthrough,
}

// classifyMediaType returns how a media type is handled.
// image/* passes through as binary whatever the subtype.
func classifyMediaType(mediaType string) mediaKind {
	if kind, ok := mediaKinds[mediaType]; ok {
		return kind
	}
	if strings.HasPrefix(mediaType, "image/") {
		return mediaKindPassthrough
	}
	return mediaKindUnsupported
}

// openHTTP handles an HTTP(S) URI by Content-Type and returns a read stream.
// All fetching goes through fetchBytes, so the response size cap applies equally to every Content-Type.
func (r *UniversalReader) openHTTP(ctx context.Context, uri string) (io.ReadCloser, error) {
	got, err := r.fetchBytes(ctx, uri)
	if err != nil {
		return nil, err
	}

	contentType, err := resolveMediaType(got.contentType)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Content-Type: %w", err)
	}

	switch classifyMediaType(contentType) {
	case mediaKindHTML:
		// Pass the raw header to the extractor; the charset is in the charset parameter.
		return r.openExtractedHTML(ctx, uri, bytes.NewReader(got.body), got.contentType)
	case mediaKindPassthrough:
		return io.NopCloser(bytes.NewReader(got.body)), nil
	default:
		if contentType == "" {
			return nil, fmt.Errorf("unsupported Content-Type: %s", uri)
		}
		return nil, fmt.Errorf("unsupported Content-Type: %s (%s)", uri, contentType)
	}
}

// openExtractedHTML extracts the main text from fetched HTML and returns a read stream.
func (r *UniversalReader) openExtractedHTML(ctx context.Context, uri string, body io.Reader, contentType string) (io.ReadCloser, error) {
	text, hasBody, err := r.extractText(ctx, body, contentType)
	if err != nil {
		return nil, err
	}
	if !hasBody {
		return nil, fmt.Errorf("no content found: %s", uri)
	}

	return io.NopCloser(strings.NewReader(text)), nil
}

// extractText extracts, passing the Content-Type along when the extractor accepts it.
func (r *UniversalReader) extractText(ctx context.Context, body io.Reader, contentType string) (string, bool, error) {
	if extractor, ok := r.extractor.(ContentTypeExtractor); ok {
		return extractor.ExtractWithContentType(ctx, body, contentType)
	}
	return r.extractor.Extract(ctx, body)
}

// resolveMediaType extracts only the media type from a Content-Type header.
//
// A header that does not follow the RFC (such as an unclosed quote around charset, which real
// servers do return) is still accepted when the part before ";" is a known media type. Rescuing
// unknown ones too would misinterpret the content on the strength of a broken header, so those
// return the parse error.
//
// Only values shaped "type/subtype" are rescued. classifyMediaType passes image/ by prefix match,
// so without this check a broken value such as "image/ 0" would be handled as an image (and the
// return value would be a string unusable as a media type).
func resolveMediaType(contentType string) (string, error) {
	if contentType == "" {
		return "", nil
	}

	parsed, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		return parsed, nil
	}

	normalized := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.IndexByte(normalized, ';'); i >= 0 {
		normalized = strings.TrimSpace(normalized[:i])
	}
	if isMediaTypeToken(normalized) && classifyMediaType(normalized) != mediaKindUnsupported {
		return normalized, nil
	}

	return "", err
}

// isMediaTypeToken reports whether the value has the shape "type/subtype".
// Following RFC 9110's token, it allows no whitespace, delimiters or control characters.
func isMediaTypeToken(mediaType string) bool {
	slash := strings.IndexByte(mediaType, '/')
	if slash <= 0 || slash == len(mediaType)-1 {
		return false
	}
	for i := range len(mediaType) {
		if c := mediaType[i]; c != '/' && !isTokenChar(c) {
			return false
		}
	}
	return strings.IndexByte(mediaType[slash+1:], '/') < 0
}

// isTokenChar reports whether c is a character that can make up an RFC 9110 token.
func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		return true
	}
	return false
}
