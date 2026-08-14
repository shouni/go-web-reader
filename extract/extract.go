// Package extract は、HTMLコンテンツから本文テキストを高精度に抽出します。
//
// 取得（HTTP アクセス）はこのパッケージの責務ではありません。呼び出し側が
// 用意した io.Reader を受け取って解析するだけなので、HTTP でもファイルでも
// テスト用の文字列でも同じ経路で扱えます。
package extract

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

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
	noiseSelectors = "script, style, form, nav, aside, .related-posts, .social-share, .comments, .ad-banner, .advertisement"

	// pageFrameSelectors は、本文候補が見つからずページ全体を本文として扱うときにだけ
	// 取り除く囲み要素です。noiseSelectors と分けているのは、記事の内側の <header> が
	// 見出しを、<footer> が署名を持つことがあり、常に落とすと本文が欠けるためです。
	pageFrameSelectors = "header, footer, .sidebar"

	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "
)

// blockTags は走査対象のブロック要素です。走査用のセレクタと ownText の
// 入れ子スキップ判定の両方をここから導出するので、一覧はこの 1 箇所だけです。
var blockTags = []string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "table", "pre"}

var headingTags = []string{"h1", "h2", "h3", "h4", "h5", "h6"}

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

// Engine は状態を持たない抽出エンジンです。ゼロ値のまま使えます。
//
// 中身は Text と同じで、抽出エンジンを差し替えられる形（インターフェース）で
// 受け取りたい呼び出し側のために型として公開しています。単に一度抽出したいだけなら
// Text をそのまま呼んでください。
type Engine struct{}

// Extract は取得済みのHTMLコンテンツから整形されたテキストを抽出します。
func (Engine) Extract(ctx context.Context, r io.Reader) (string, bool, error) {
	return Text(ctx, r)
}

// Text は取得済みのHTMLコンテンツから整形されたテキストを抽出します。
//
// 第2戻り値は本文が見つかったかどうかです。タイトルしか取れなかった場合は
// テキストを返しつつ false になります（エラーではありません）。
func Text(ctx context.Context, r io.Reader) (text string, hasBodyFound bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return "", false, fmt.Errorf("HTML解析に失敗しました: %w", err)
	}

	return extractContentText(doc)
}

// extractContentText はgoquery.Documentから本文とタイトルを抽出し、整形します。
func extractContentText(doc *goquery.Document) (text string, hasBodyFound bool, err error) {
	var parts []string

	// タイトルは <head> にあるため、本文の絞り込みより先に取ります。
	pageTitle := strings.TrimSpace(doc.FindMatcher(titleMatcher).First().Text())
	if pageTitle != "" {
		parts = append(parts, titlePrefix+pageTitle)
	}

	// ノイズは本文候補を決める前にページ全体から落とします。あとから落とすと、
	// <aside> の中の <article> を本文として選んでしまう余地が残ります。
	doc.FindMatcher(noiseMatcher).Remove()

	// ブロック要素は DOM の出現順（深さ優先）に一度ずつ訪問されます。入れ子の
	// 親子が両方一致した場合は両方が訪問されるため、二重に出さない責任は
	// processGeneralElement 側にあります。
	findMainContent(doc).FindMatcher(blockMatcher).Each(func(_ int, s *goquery.Selection) {
		var content string

		switch tagName(s) {
		case "table":
			content = processTable(s)
		case "pre":
			// pre タグ (コードブロック) はコードフェンスで囲む
			if preText := strings.TrimSpace(s.Text()); preText != "" {
				content = "```\n" + preText + "\n```"
			}
		default:
			// 一般的なテキスト要素 (p, h*, li, blockquote)
			content = processGeneralElement(s)
		}

		if content != "" {
			parts = append(parts, content)
		}
	})

	return validateAndFormatResult(parts)
}

// findMainContent は本文が入っている範囲を返します。
func findMainContent(doc *goquery.Document) *goquery.Selection {
	if mainContent := doc.FindMatcher(mainContentMatcher).First(); mainContent.Length() > 0 {
		return mainContent
	}

	// 本文候補が無いページはページ全体を本文として扱い、囲み要素を DOM から取り除きます。
	// goquery の Not は「選択中のノード自身」を絞り込むだけで子孫には効かないため、
	// 以前の doc.Not(...) はドキュメントノードをそのまま返すだけで何も除外できておらず、
	// ナビゲーションやフッターのリンクが <li> として本文に混ざっていました。
	body := doc.FindMatcher(bodyMatcher).First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	body.FindMatcher(pageFrameMatcher).Remove()

	return body
}

// processGeneralElement は一般的なテキスト要素からテキストを抽出し、整形します。
func processGeneralElement(s *goquery.Selection) string {
	content := normalizeSpace(ownText(s))
	if content == "" {
		return ""
	}

	tag := tagName(s)
	if _, isHeading := headingTagSet[tag]; isHeading {
		if utf8.RuneCountInString(content) >= MinHeadingLength {
			return "## " + content
		}
		return ""
	}

	// リスト項目は短くても項目として意味を持つため、長さで落としません。
	if tag == "li" || utf8.RuneCountInString(content) >= MinParagraphLength {
		return content
	}
	return ""
}

// ownText は s 配下のテキストのうち、s 自身が担当する分だけを連結します。
//
// 子孫のブロック要素（blockSelectors）は走査対象として別途訪問されるため、ここでは
// 中身に立ち入りません。除外しないと <li><p>…</p></li> のような入れ子で
// 親と子の両方が同じ文を出力し、本文が二重になります。
func ownText(s *goquery.Selection) string {
	var builder strings.Builder
	for _, node := range s.Nodes {
		writeOwnText(&builder, node)
	}
	return builder.String()
}

// writeOwnText は n の子孫のテキストを、ブロック要素の内側を除いて builder に書き出します。
//
// goquery を介さず html.Node を直接辿ります。Contents().Each(...) は子ノードごとに
// Selection を確保するうえ、Is(セレクタ文字列) はノードごとにセレクタを
// コンパイルし直すため、文書全体を歩くこの経路では割に合いません。
func writeOwnText(builder *strings.Builder, n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			builder.WriteString(child.Data)
		case html.ElementNode:
			if _, isBlock := blockTagSet[child.Data]; isBlock {
				continue
			}
			writeOwnText(builder, child)
		}
		// コメントノードやDOCTYPEなどは無視
	}
}

// normalizeSpace は連続する空白（改行やタブを含む）を単一のスペースにまとめ、
// 前後の空白を落とします。HTML のインデントがそのまま本文に出るのを防ぎます。
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// processTable は goquery.Selection からテーブルの内容を抽出し、整形します。
func processTable(s *goquery.Selection) string {
	var tableContent []string
	captionText := strings.TrimSpace(s.FindMatcher(captionMatcher).First().Text())
	if captionText != "" {
		tableContent = append(tableContent, tableCaptionPrefix+captionText)
	}
	s.FindMatcher(rowMatcher).Each(func(_ int, row *goquery.Selection) {
		var rowTexts []string
		hasValue := false
		row.FindMatcher(cellMatcher).Each(func(_ int, cell *goquery.Selection) {
			cellText := normalizeSpace(cell.Text())
			if cellText != "" {
				hasValue = true
			}
			rowTexts = append(rowTexts, cellText)
		})
		// セルの無い行や全セルが空の行は、出力では空行にしかなりません。
		if !hasValue {
			return
		}
		tableContent = append(tableContent, strings.Join(rowTexts, " | "))
	})
	if len(tableContent) > 0 {
		return strings.Join(tableContent, "\n")
	}
	return ""
}

// validateAndFormatResult は抽出結果を 1 本のテキストに連結します。
// タイトルしか取れなかった場合は、テキストを返しつつ本文なしとして報告します。
func validateAndFormatResult(parts []string) (text string, hasBodyFound bool, err error) {
	if len(parts) == 0 {
		return "", false, fmt.Errorf("webページから何も抽出できませんでした")
	}
	isTitleOnly := len(parts) == 1 && strings.HasPrefix(parts[0], titlePrefix)
	if isTitleOnly {
		return parts[0], false, nil
	}
	return strings.Join(parts, "\n\n"), true, nil
}
