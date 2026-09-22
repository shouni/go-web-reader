package extract

import (
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

const (
	// MinParagraphLength は、本文として採用する段落の最小文字数（バイト数ではない）です。
	MinParagraphLength = 20
	// MinHeadingLength は、見出しとして採用する最小文字数です。「概要」のような 2 文字の見出しを残します。
	MinHeadingLength = 2

	// mainContentSelectors は本文が入っていそうな要素です。最初に一致したものを本文候補にします。
	mainContentSelectors = "article, main, div[role='main'], #main, #content, .post-content, .article-body, .entry-content, .markdown-body, .readme"

	// noiseSelectors は本文の内側にあっても本文ではない要素で、本文候補を決める前に
	// ページ全体から取り除きます。noscript / template はパーサからは中身がただのテキストに
	// 見えるため、落とさないと段落に混ざります。hidden / aria-hidden は文書自身が非表示を宣言した要素です。
	noiseSelectors = "script, style, form, nav, aside, noscript, template, [hidden], [aria-hidden='true'], .related-posts, .social-share, .comments, .ad-banner, .advertisement"

	// pageFrameSelectors は、本文候補が無くページ全体を本文とするときにだけ取り除く囲み要素です。
	// 記事の内側の <header> は見出しを、<footer> は署名を持つことがあるため、常には落としません。
	pageFrameSelectors = "header, footer, .sidebar"

	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "
)

// blockTags は走査対象のブロック要素です。blockMatcher と blockTagSet を
// ここから導出するので、一覧はこの 1 箇所だけです。
var blockTags = []string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dt", "dd", "figcaption", "blockquote", "table", "pre"}

var headingTags = []string{"h1", "h2", "h3", "h4", "h5", "h6"}

// tableTags は表の構造要素です。セルの平坦化でブロック要素と同じく境界に空白を挟みます。
var tableTags = []string{"caption", "thead", "tbody", "tfoot", "tr", "th", "td"}

// shortTags は段落の最小文字数を課さないブロック要素です。短くても項目として意味を持ちます。
var shortTags = []string{"li", "dt", "dd", "figcaption"}

// セレクタは 1 度だけコンパイルします。goquery の文字列を取る Find/Is は
// 呼ぶたびに cascadia.Compile を呼び直し、キャッシュしません。
var (
	blockMatcher       = cascadia.MustCompile(strings.Join(blockTags, ", "))
	mainContentMatcher = cascadia.MustCompile(mainContentSelectors)
	noiseMatcher       = cascadia.MustCompile(noiseSelectors)
	pageFrameMatcher   = cascadia.MustCompile(pageFrameSelectors)
	titleMatcher       = cascadia.MustCompile("title")
	bodyMatcher        = cascadia.MustCompile("body")

	// containerMatcher は、段落要素を使わずに本文を直書きするページのための走査対象です。
	// ブロック要素が 1 つも本文を出さなかったときにだけ使います（extract.go の
	// collectContainerParagraphs）。
	containerMatcher = cascadia.MustCompile("div, section, article")

	blockTagSet   = newTagSet(blockTags)
	headingTagSet = newTagSet(headingTags)
	shortTagSet   = newTagSet(shortTags)
	// flattenBoundarySet は表のセルを平坦化するときに空白で区切る要素です。
	flattenBoundarySet = newTagSet(slices.Concat(blockTags, tableTags))
)

func newTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	return set
}

// tagName は要素ノードのタグ名を返します。要素でなければ空文字列です。
// 単一タグの判定に CSS の照合機構は要りません。
func tagName(s *goquery.Selection) string {
	node := s.Get(0)
	if node == nil || node.Type != html.ElementNode {
		return ""
	}
	return node.Data
}
