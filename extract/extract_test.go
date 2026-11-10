package extract_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

const (
	// must match the implementation's constants exactly
	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "

	// a paragraph long enough to be extracted as body text
	longParagraph = "This is a long paragraph with more than twenty characters and it should be extracted as body content."
)

func TestText(t *testing.T) {
	testCases := []struct {
		name              string
		html              string
		expectedText      string
		expectedBodyFound bool
		expectedError     bool
	}{
		{
			// short text is not considered body text, so only the title remains
			name:              "document_with_title_only",
			html:              `<html><head><title>Test Title</title></head><body><p>Short text</p></body></html>`,
			expectedText:      titlePrefix + "Test Title",
			expectedBodyFound: false,
		},
		{
			// A page that writes its body with <div> and <br> and no paragraph elements. Only when no
			// block produces body text is the direct text of leaf containers picked up as paragraphs.
			name:              "document_with_div_and_br_body",
			html:              fmt.Sprintf(`<html><head><title>Old Blog</title></head><body><article><div class="entry">%s<br><br>%s<br>short</div></article></body></html>`, longParagraph, longParagraph),
			expectedText:      titlePrefix + "Old Blog" + "\n\n" + longParagraph + "\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			// If even one block element produces body text, container asides are not picked up.
			name:              "document_with_p_ignores_div_text",
			html:              fmt.Sprintf(`<html><head><title>Mixed</title></head><body><main><div>%s</div><p>%s</p></main></body></html>`, "箱要素に直接書かれた二十文字を超える端書きの文章です。", longParagraph),
			expectedText:      titlePrefix + "Mixed" + "\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			name:              "document_with_main_content_and_title",
			html:              fmt.Sprintf(`<html><head><title>Title</title></head><body><main><p>%s</p></main></body></html>`, longParagraph),
			expectedText:      titlePrefix + "Title" + "\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			name: "document_with_headings_and_paragraphs",
			html: fmt.Sprintf(`<html><head><title>Test Page</title></head><body><article>
                <h1>Heading 1 Long Enough Title</h1>
                <p>Short</p>
                <h2>H2 Long Enough</h2>
                <p>%s</p>
               </article></body></html>`, longParagraph),
			expectedText: titlePrefix + "Test Page" + "\n\n" +
				"## Heading 1 Long Enough Title" + "\n\n" +
				"## H2 Long Enough" + "\n\n" +
				longParagraph,
			expectedBodyFound: true,
		},
		{
			// "Intro text" is shorter than MinParagraphLength, so it is ignored
			name: "document_with_table_and_pre",
			html: `<html><head><title>Code Table</title></head><body><main>
                   <article>
                      <p>Intro text</p>
                      <table><caption>Data Table</caption><tr><td>Col1</td><td>Val1</td></tr></table>
                      <pre>func hello() {}</pre>
                   </article>
                   </main></body></html>`,
			expectedText: titlePrefix + "Code Table" + "\n\n" +
				tableCaptionPrefix + "Data Table" + "\nCol1 | Val1" + "\n\n" +
				"```\nfunc hello() {}\n```",
			expectedBodyFound: true,
		},
		{
			// list items are extracted even when short
			name: "document_with_list_items",
			html: `<html><head><title>List Test</title></head><body><main><ul><li>Item 1</li><li>Item 2</li></ul></main></body></html>`,
			expectedText: titlePrefix + "List Test" + "\n\n" +
				"Item 1" + "\n\n" +
				"Item 2",
			expectedBodyFound: true,
		},
		{
			name:          "empty_document_error",
			html:          `<html><head><title></title></head><body></body></html>`,
			expectedError: true,
		},
		{
			// On a page without article/main, the whole page must be treated as the body while
			// navigation and footers are dropped.
			name: "page_without_main_drops_navigation_and_footer",
			html: `<html><head><title>No Main</title></head><body>
                   <nav><ul><li>Home</li><li>About</li></ul></nav>
                   <p>` + longParagraph + `</p>
                   <footer><ul><li>Privacy</li></ul></footer>
                   </body></html>`,
			expectedText:      titlePrefix + "No Main" + "\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			// Nested block elements must not be emitted twice, by parent and child.
			name: "nested_blocks_are_not_duplicated",
			html: `<html><head><title>Nested</title></head><body><main>
                   <ul><li><p>` + longParagraph + `</p></li></ul>
                   <blockquote><p>This quoted paragraph is long enough to be kept.</p></blockquote>
                   </main></body></html>`,
			expectedText: titlePrefix + "Nested" + "\n\n" +
				longParagraph + "\n\n" +
				"This quoted paragraph is long enough to be kept.",
			expectedBodyFound: true,
		},
		{
			// Thresholds must be measured in characters. Measured in bytes, 10 characters (30 bytes)
			// of Japanese would exceed the threshold of 20 and survive as body text.
			name:              "japanese_paragraph_is_measured_in_runes",
			html:              `<html><head><title>日本語</title></head><body><main><p>短い文です。ノイズ。</p></main></body></html>`,
			expectedText:      titlePrefix + "日本語",
			expectedBodyFound: false,
		},
		{
			// A two-character Japanese heading must survive (the MinHeadingLength floor).
			name: "short_japanese_heading_is_kept",
			html: `<html><head><title>見出し</title></head><body><main>
                   <h2>概要</h2><p>` + longParagraph + `</p>
                   </main></body></html>`,
			expectedText:      titlePrefix + "見出し" + "\n\n## 概要\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			// A row with no cells must be dropped, not turned into a blank line.
			name: "table_skips_rows_without_cells",
			html: `<html><head><title>Table</title></head><body><main>
                   <p>` + longParagraph + `</p>
                   <table><tbody><tr></tr><tr><td>A</td><td>B</td></tr></tbody></table>
                   </main></body></html>`,
			expectedText: titlePrefix + "Table" + "\n\n" +
				longParagraph + "\n\n" +
				"A | B",
			expectedBodyFound: true,
		},
		{
			// Paragraphs and lists inside cells must come out only as table rows, not again outside the table.
			// List items are flattened with a separator (not fused into "AB").
			name: "table_flattens_nested_blocks_once",
			html: `<html><head><title>Table</title></head><body><main>
                   <p>` + longParagraph + `</p>
                   <table><tr>
                     <td><p>` + longParagraph + `</p></td>
                     <td><ul><li>A</li><li>B</li></ul></td>
                   </tr></table>
                   </main></body></html>`,
			expectedText: titlePrefix + "Table" + "\n\n" +
				longParagraph + "\n\n" +
				longParagraph + " | A B",
			expectedBodyFound: true,
		},
		{
			// A nested table must come out once, as the text of the outer cell, and its rows and cells
			// must not mix into the outer rows.
			name: "nested_table_is_emitted_once",
			html: `<html><head><title>Table</title></head><body><main>
                   <p>` + longParagraph + `</p>
                   <table><caption>Outer</caption><tbody><tr>
                     <td>Left</td>
                     <td><table><caption>Inner</caption><tr><td>X</td><td>Y</td></tr></table></td>
                   </tr></tbody></table>
                   </main></body></html>`,
			expectedText: titlePrefix + "Table" + "\n\n" +
				longParagraph + "\n\n" +
				tableCaptionPrefix + "Outer" + "\nLeft | Inner X Y",
			expectedBodyFound: true,
		},
		{
			// <br> is a line break itself, so the lines on either side must not fuse into one word.
			name: "br_separates_adjacent_lines",
			html: `<html><head><title>Break</title></head><body><main>
                   <p>The quick brown fox<br>jumps over the lazy dog.</p>
                   </main></body></html>`,
			expectedText:      titlePrefix + "Break" + "\n\n" + "The quick brown fox jumps over the lazy dog.",
			expectedBodyFound: true,
		},
		{
			// The parser sees noscript's contents as plain text, so unless it is dropped it mixes into
			// the enclosing paragraph's text.
			name: "noscript_and_hidden_elements_are_dropped",
			html: `<html><head><title>Noise</title></head><body><main>
                   <p><noscript>JavaScriptを有効にしてください。</noscript>` + longParagraph + `</p>
                   <p hidden>` + longParagraph + ` hidden.</p>
                   <p aria-hidden="true">` + longParagraph + ` aria.</p>
                   </main></body></html>`,
			expectedText:      titlePrefix + "Noise" + "\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			// Definition lists and figure captions must be picked up as body text too.
			// Like li, they carry meaning as items even when short, so length does not drop them.
			name: "definition_list_and_figcaption_are_extracted",
			html: `<html><head><title>Terms</title></head><body><main>
                   <dl><dt>SSRF</dt><dd>Server-Side Request Forgery</dd></dl>
                   <figure><figcaption>図1</figcaption></figure>
                   </main></body></html>`,
			expectedText: titlePrefix + "Terms" + "\n\n" +
				"SSRF" + "\n\n" +
				"Server-Side Request Forgery" + "\n\n" +
				"図1",
			expectedBodyFound: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualText, actualBodyFound, err := extract.Text(t.Context(), strings.NewReader(tc.html))

			if tc.expectedError {
				if err == nil {
					t.Fatalf("expected an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actualBodyFound != tc.expectedBodyFound {
				t.Errorf("hasBodyFound = %v, want %v", actualBodyFound, tc.expectedBodyFound)
			}
			if actualText != tc.expectedText {
				t.Errorf("extracted text differs from the expected text\n got: %q\nwant: %q", actualText, tc.expectedText)
			}
		})
	}
}

// TestEngineDelegatesToText checks that the DI Engine, as a zero value, returns the same result as
// Text.
func TestEngineDelegatesToText(t *testing.T) {
	body := "This paragraph is long enough to be treated as extracted article body."
	doc := fmt.Sprintf(`<html><head><title>Reader Title</title></head><body><main><p>%s</p></main></body></html>`, body)
	want := titlePrefix + "Reader Title" + "\n\n" + body

	actualText, actualBodyFound, err := extract.Engine{}.Extract(t.Context(), strings.NewReader(doc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !actualBodyFound {
		t.Errorf("hasBodyFound = false, want true")
	}
	if actualText != want {
		t.Errorf("extracted text differs from the expected text\n got: %q\nwant: %q", actualText, want)
	}
}

// TestTextRespectsCanceledContext checks that cancellation is detected before parsing starts.
func TestTextRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, _, err := extract.Text(ctx, strings.NewReader(`<html><body><p>ignored</p></body></html>`)); err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
}
