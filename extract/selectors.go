package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

const (
	// MinParagraphLength は、本文として採用する段落の最小文字数です。
	// バイト数ではなく文字数で測ります。len() だと日本語は 1 文字 3 バイトのため
	// しきい値が実質 1/3 になり、短いノイズが本文として残ります。
	MinParagraphLength = 20
	// MinHeadingLength は、見出しとして採用する最小文字数です。
	// 「概要」のような 2 文字の見出しを落とさない値にしています。
	MinHeadingLength = 2

	// mainContentSelectors は本文が入っていそうな要素です。最初に一致したものを本文候補にします。
	mainContentSelectors = "article, main, div[role='main'], #main, #content, .post-content, .article-body, .entry-content, .markdown-body, .readme"

	// noiseSelectors は本文の内側にあっても本文ではない要素です。
	// 本文候補を決める前にページ全体から取り除きます。
	//
	// noscript / template は、パーサからは中身が「ただのテキスト」に見えるため、
	// 落とさないと囲っている段落の本文に混ざります。hidden / aria-hidden は
	// 画面に出さないことを文書自身が宣言している要素なので同様に扱います。
	noiseSelectors = "script, style, form, nav, aside, noscript, template, [hidden], [aria-hidden='true'], .related-posts, .social-share, .comments, .ad-banner, .advertisement"

	// pageFrameSelectors は、本文候補が見つからずページ全体を本文として扱うときにだけ
	// 取り除く囲み要素です。noiseSelectors と分けているのは、記事の内側の <header> が
	// 見出しを、<footer> が署名を持つことがあり、常に落とすと本文が欠けるためです。
	pageFrameSelectors = "header, footer, .sidebar"

	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "
)

// blockTags は走査対象のブロック要素です。走査用のセレクタと ownText の
// 入れ子スキップ判定の両方をここから導出するので、一覧はこの 1 箇所だけです。
var blockTags = []string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dt", "dd", "figcaption", "blockquote", "table", "pre"}

var headingTags = []string{"h1", "h2", "h3", "h4", "h5", "h6"}

// shortTags は段落の最小文字数を課さないブロック要素です。
// リスト項目・定義語・図のキャプションは、短くてもそれ自体で意味を持ちます。
var shortTags = []string{"li", "dt", "dd", "figcaption"}

// セレクタは起動時に 1 度だけコンパイルします。goquery の Find/Is は
// 文字列を受け取るたびに cascadia.Compile を呼び直すため、ノードごとに
// 判定する箇所でそのまま使うと、走査のたびにセレクタを解析し直すことになります。
var (
	blockMatcher       = cascadia.MustCompile(strings.Join(blockTags, ", "))
	mainContentMatcher = cascadia.MustCompile(mainContentSelectors)
	noiseMatcher       = cascadia.MustCompile(noiseSelectors)
	pageFrameMatcher   = cascadia.MustCompile(pageFrameSelectors)
	titleMatcher       = cascadia.MustCompile("title")
	bodyMatcher        = cascadia.MustCompile("body")
	captionMatcher     = cascadia.MustCompile("caption")
	rowMatcher         = cascadia.MustCompile("tr")
	cellMatcher        = cascadia.MustCompile("th, td")

	blockTagSet   = newTagSet(blockTags)
	headingTagSet = newTagSet(headingTags)
	shortTagSet   = newTagSet(shortTags)
)

func newTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	return set
}

// tagName は要素ノードのタグ名を返します。要素でなければ空文字列です。
//
// 単一タグの判定にセレクタを使わないのは、CSS の照合機構を通さずとも
// html.Node のタグ名を直接見れば足りるためです。
func tagName(s *goquery.Selection) string {
	node := s.Get(0)
	if node == nil || node.Type != html.ElementNode {
		return ""
	}
	return node.Data
}
