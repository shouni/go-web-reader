// Package extract extracts the main text from HTML content with high precision.
//
// Fetching (HTTP access) is not this package's responsibility. It only parses the io.Reader the caller
// provides, so HTTP, a file and a test string all take the same path.
//
// # Extraction rules
//
// A heuristic aimed at keeping only the article body, applied in this order.
//
// 0. Detect the charset. It is determined in the order BOM → Content-Type charset → <meta charset> →
// the body bytes, and converted to UTF-8. The parser assumes UTF-8 input, so without this a
// non-UTF-8 page comes out entirely garbled. If you have the Content-Type header, pass it to
// TextWithContentType.
//
// 1. Drop noise. script, style, form, nav, aside, noscript, template,
// [hidden], [aria-hidden="true"], and the classes around ads, social buttons and comments
// (.related-posts, .social-share, .comments, .ad-banner, .advertisement) are removed
// from the whole page. The parser sees the contents of noscript / template as plain
// text, so unless they are dropped they mix into the enclosing paragraph's text.
//
// 2. Decide the extent of the body. The first element matching article, main, div[role='main'],
// #main, #content, .post-content, .article-body, .entry-content, .markdown-body or .readme
// is the body. If none is found the whole page is taken as the body, and only then are
// header / footer / .sidebar dropped too (inside an article a header may hold the heading and a
// footer the byline, so always dropping them would lose part of the body).
//
// 3. Pick up block elements in order. p, h1-h6, li, dt, dd, figcaption, blockquote,
// table and pre are scanned in DOM order. Nesting (<li><p>…</p></li> and the like) is
// emitted only once, and a table's contents come out only as table rows. <br> is treated as a space.
// The output looks like this:
//
//   - title — first, prefixed with 「【記事タイトル】 」
//   - h1-h6 — prefixed with "## " (only those of at least MinHeadingLength characters)
//   - p, blockquote — only those of at least MinParagraphLength characters
//   - li, dt, dd, figcaption — emitted at any length (they carry meaning as items even when short)
//   - table — a caption prefixed with 「【表題】 」 and "cell | cell" rows. Cell contents are
//     flattened at any length, and empty rows are not emitted
//   - pre — wrapped in a code fence
//
// Thresholds are measured in characters, not bytes. With len(), Japanese takes 3 bytes per character,
// so the thresholds would effectively be one third and navigation fragments would survive as body text.
package extract

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
)

// Engine is a stateless extraction engine. Its zero value is ready to use.
// It does the same as Text, and exists for callers that want to pass it as an interface value.
type Engine struct{}

// Extract extracts formatted text from already-fetched HTML content.
func (Engine) Extract(ctx context.Context, r io.Reader) (string, bool, error) {
	return Text(ctx, r)
}

// ExtractWithContentType is Extract with the Content-Type header supplied.
// It is more accurate than Extract to the extent that the header's charset can be used to detect the charset.
func (Engine) ExtractWithContentType(ctx context.Context, r io.Reader, contentType string) (string, bool, error) {
	return TextWithContentType(ctx, r, contentType)
}

// Text extracts formatted text from already-fetched HTML content.
//
// The second return value reports whether a body was found. When only the title could be taken,
// the text is returned and the value is false (this is not an error).
func Text(ctx context.Context, r io.Reader) (text string, hasBodyFound bool, err error) {
	return TextWithContentType(ctx, r, "")
}

// TextWithContentType is Text with the value of the Content-Type header supplied.
//
// For the charset, the header's charset takes precedence over <meta charset>. For a Shift_JIS /
// EUC-JP page that does not declare it in <meta>, the header is the only clue, so pass it if you
// have it. An empty contentType behaves like Text.
func TextWithContentType(ctx context.Context, r io.Reader, contentType string) (text string, hasBodyFound bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	// The parser assumes UTF-8 input, so convert first.
	decoded, err := charset.NewReader(r, contentType)
	if err != nil {
		return "", false, fmt.Errorf("failed to detect charset: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(decoded)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse HTML: %w", err)
	}

	return extractContentText(doc)
}

// extractContentText extracts and formats the body and title from a goquery.Document.
func extractContentText(doc *goquery.Document) (text string, hasBodyFound bool, err error) {
	var parts []string

	// The title is in <head>, so take it before narrowing down to the body.
	pageTitle := normalizeSpace(doc.FindMatcher(titleMatcher).First().Text())
	if pageTitle != "" {
		parts = append(parts, titlePrefix+pageTitle)
	}

	// Noise is dropped from the whole page before the main-content candidate is chosen. Dropping it
	// afterwards would leave room to pick an <article> inside an <aside> as the body.
	doc.FindMatcher(noiseMatcher).Remove()

	// Nested blocks are visited as both parent and child. Not emitting them twice is ownText's job;
	// only a table's contents are emitted by the table as rows, so they are skipped here.
	mainContent := findMainContent(doc)
	bodyStart := len(parts)
	mainContent.FindMatcher(blockMatcher).Each(func(_ int, s *goquery.Selection) {
		if insideTable(s.Get(0)) {
			return
		}
		if content := processBlock(s); content != "" {
			parts = append(parts, content)
		}
	})

	// A page that writes its body with <div> and <br> and no paragraph elements at all (old blogs,
	// hand-written CMS output) is lost entirely by the scan above. Only when no block produced any
	// body text, pick up the direct text of leaf containers as paragraphs. If there is even one block,
	// trust it instead (so that a mixed page's container asides are not picked up).
	if len(parts) == bodyStart {
		parts = append(parts, collectContainerParagraphs(mainContent)...)
	}

	return validateAndFormatResult(parts)
}

// collectContainerParagraphs collects the direct text of containers (div / section / article) with no
// block-element descendants as paragraphs, one per <br>-separated line.
// Lines shorter than MinParagraphLength are dropped by the same rule as <p>.
func collectContainerParagraphs(root *goquery.Selection) []string {
	var parts []string
	root.FindMatcher(containerMatcher).Each(func(_ int, s *goquery.Selection) {
		if insideTable(s.Get(0)) {
			return
		}
		// For nested containers, look only at the innermost. Picking up the outer one too emits the same text twice.
		if s.FindMatcher(containerMatcher).Length() > 0 || s.FindMatcher(blockMatcher).Length() > 0 {
			return
		}
		var builder strings.Builder
		for _, node := range s.Nodes {
			writeTextWithBreaks(&builder, node)
		}
		for line := range strings.SplitSeq(builder.String(), "\n") {
			if content := normalizeSpace(line); utf8.RuneCountInString(content) >= MinParagraphLength {
				parts = append(parts, content)
			}
		}
	})
	return parts
}

// findMainContent returns the part of the document that holds the body.
func findMainContent(doc *goquery.Document) *goquery.Selection {
	if mainContent := doc.FindMatcher(mainContentMatcher).First(); mainContent.Length() > 0 {
		return mainContent
	}

	// With no main-content candidate, take the whole page as the body and remove the framing elements
	// from the DOM. goquery's Not filters only the selected nodes themselves, not their descendants, so
	// Remove is used.
	body := doc.FindMatcher(bodyMatcher).First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	body.FindMatcher(pageFrameMatcher).Remove()

	return body
}

// processBlock returns the output for one block element, or an empty string when nothing is emitted.
func processBlock(s *goquery.Selection) string {
	switch tagName(s) {
	case "table":
		return processTable(s)
	case "pre":
		// Preformatted text: skip normalizeSpace and keep the indentation.
		preText := strings.TrimSpace(s.Text())
		if preText == "" {
			return ""
		}
		return "```\n" + preText + "\n```"
	default:
		return processGeneralElement(s)
	}
}

// validateAndFormatResult joins the extraction results into one text.
// When only the title could be taken, it returns the text but reports that no body was found.
func validateAndFormatResult(parts []string) (text string, hasBodyFound bool, err error) {
	if len(parts) == 0 {
		return "", false, fmt.Errorf("nothing could be extracted from the web page")
	}
	isTitleOnly := len(parts) == 1 && strings.HasPrefix(parts[0], titlePrefix)
	if isTitleOnly {
		return parts[0], false, nil
	}
	return strings.Join(parts, "\n\n"), true, nil
}
