package extract_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

const (
	// 本体コードの定数と完全に一致させる
	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "

	// 本文として抽出されるための十分な長さを持つパラグラフ
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
			// 短いテキストは本文と見なされないため、タイトルだけが残る
			name:              "document_with_title_only",
			html:              `<html><head><title>Test Title</title></head><body><p>Short text</p></body></html>`,
			expectedText:      titlePrefix + "Test Title",
			expectedBodyFound: false,
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
			// "Intro text" は MinParagraphLength より短いため無視される
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
			// リストアイテムは短くても抽出される
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
			// article/main が無いページでは、ページ全体を本文として扱いつつ
			// ナビゲーションやフッターを落とすこと。以前は goquery の Not が
			// 子孫に効かず、リンクが <li> として本文に混ざっていた。
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
			// 入れ子のブロック要素は親と子で二重に出力しないこと。
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
			// しきい値は文字数で測ること。バイト数で測ると 10 文字（30 バイト）の
			// 日本語がしきい値 20 を超えてしまい、本文として残る。
			name:              "japanese_paragraph_is_measured_in_runes",
			html:              `<html><head><title>日本語</title></head><body><main><p>短い文です。ノイズ。</p></main></body></html>`,
			expectedText:      titlePrefix + "日本語",
			expectedBodyFound: false,
		},
		{
			// 2 文字の日本語見出しは残ること（MinHeadingLength の下限）。
			name: "short_japanese_heading_is_kept",
			html: `<html><head><title>見出し</title></head><body><main>
                   <h2>概要</h2><p>` + longParagraph + `</p>
                   </main></body></html>`,
			expectedText:      titlePrefix + "見出し" + "\n\n## 概要\n\n" + longParagraph,
			expectedBodyFound: true,
		},
		{
			// セルの無い行は空行にせず落とすこと。
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualText, actualBodyFound, err := extract.Text(t.Context(), strings.NewReader(tc.html))

			if tc.expectedError {
				if err == nil {
					t.Fatalf("エラーが期待されていましたが、エラーがありませんでした")
				}
				return
			}
			if err != nil {
				t.Fatalf("予期せぬエラーが発生しました: %v", err)
			}
			if actualBodyFound != tc.expectedBodyFound {
				t.Errorf("hasBodyFound = %v, want %v", actualBodyFound, tc.expectedBodyFound)
			}
			if actualText != tc.expectedText {
				t.Errorf("抽出テキストが期待値と異なります\n got: %q\nwant: %q", actualText, tc.expectedText)
			}
		})
	}
}

// TestEngineDelegatesToText は、DI 用の Engine がゼロ値のまま
// Text と同じ結果を返すことを確認します。
func TestEngineDelegatesToText(t *testing.T) {
	body := "This paragraph is long enough to be treated as extracted article body."
	doc := fmt.Sprintf(`<html><head><title>Reader Title</title></head><body><main><p>%s</p></main></body></html>`, body)
	want := titlePrefix + "Reader Title" + "\n\n" + body

	actualText, actualBodyFound, err := extract.Engine{}.Extract(t.Context(), strings.NewReader(doc))
	if err != nil {
		t.Fatalf("予期せぬエラーが発生しました: %v", err)
	}
	if !actualBodyFound {
		t.Errorf("hasBodyFound = false, want true")
	}
	if actualText != want {
		t.Errorf("抽出テキストが期待値と異なります\n got: %q\nwant: %q", actualText, want)
	}
}

// TestTextRespectsCanceledContext は、解析に入る前にキャンセルを検出することを確認します。
func TestTextRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, _, err := extract.Text(ctx, strings.NewReader(`<html><body><p>ignored</p></body></html>`)); err == nil {
		t.Fatal("キャンセル済み context ではエラーが期待されます")
	}
}
