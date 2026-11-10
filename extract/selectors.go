package extract

import (
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

const (
	// MinParagraphLength is the minimum length, in characters (not bytes), of a paragraph kept as body text.
	MinParagraphLength = 20
	// MinHeadingLength is the minimum length, in characters, of a heading that is kept. It keeps
	// two-character headings such as 「概要」.
	MinHeadingLength = 2

	// mainContentSelectors are the elements likely to hold the main content. The first match becomes the
	// main-content candidate.
	mainContentSelectors = "article, main, div[role='main'], #main, #content, .post-content, .article-body, .entry-content, .markdown-body, .readme"

	// noiseSelectors are elements that are not body text even inside the main content; they are removed
	// from the whole page before the main-content candidate is chosen. The parser sees the contents of
	// noscript / template as plain text, so unless they are dropped they mix into paragraphs.
	// hidden / aria-hidden are elements the document itself declares hidden.
	noiseSelectors = "script, style, form, nav, aside, noscript, template, [hidden], [aria-hidden='true'], .related-posts, .social-share, .comments, .ad-banner, .advertisement"

	// pageFrameSelectors are framing elements removed only when there is no main-content candidate and the
	// whole page is taken as the body. Inside an article a <header> may hold the heading and a <footer> the
	// byline, so they are not always dropped.
	pageFrameSelectors = "header, footer, .sidebar"

	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "
)

// blockTags are the block elements that are scanned. blockMatcher and blockTagSet are derived from it,
// so this is the only list.
var blockTags = []string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dt", "dd", "figcaption", "blockquote", "table", "pre"}

var headingTags = []string{"h1", "h2", "h3", "h4", "h5", "h6"}

// tableTags are a table's structural elements. Flattening a cell puts a space at their boundaries, as
// it does for block elements.
var tableTags = []string{"caption", "thead", "tbody", "tfoot", "tr", "th", "td"}

// shortTags are block elements exempt from the minimum paragraph length. They carry meaning as items even
// when short.
var shortTags = []string{"li", "dt", "dd", "figcaption"}

// Selectors are compiled once. goquery's string-taking Find/Is call cascadia.Compile again on every
// call and do not cache.
var (
	blockMatcher       = cascadia.MustCompile(strings.Join(blockTags, ", "))
	mainContentMatcher = cascadia.MustCompile(mainContentSelectors)
	noiseMatcher       = cascadia.MustCompile(noiseSelectors)
	pageFrameMatcher   = cascadia.MustCompile(pageFrameSelectors)
	titleMatcher       = cascadia.MustCompile("title")
	bodyMatcher        = cascadia.MustCompile("body")

	// containerMatcher is what is scanned for pages that write the body directly, without paragraph
	// elements. It is used only when no block element produced any body text
	// (collectContainerParagraphs in extract.go).
	containerMatcher = cascadia.MustCompile("div, section, article")

	blockTagSet   = newTagSet(blockTags)
	headingTagSet = newTagSet(headingTags)
	shortTagSet   = newTagSet(shortTags)
	// flattenBoundarySet are the elements separated by a space when a table cell is flattened.
	flattenBoundarySet = newTagSet(slices.Concat(blockTags, tableTags))
)

func newTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	return set
}

// tagName returns the tag name of an element node, or an empty string for a non-element.
// A single-tag check needs no CSS matching machinery.
func tagName(s *goquery.Selection) string {
	node := s.Get(0)
	if node == nil || node.Type != html.ElementNode {
		return ""
	}
	return node.Data
}
