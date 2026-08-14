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
	"github.com/shouni/go-utils/text"
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

	// blockSelectors は走査対象のブロック要素です。
	// この一覧は processGeneralElement の入れ子スキップ判定にも使われます。
	blockSelectors = "p, h1, h2, h3, h4, h5, h6, li, blockquote, table, pre"

	headingSelectors = "h1, h2, h3, h4, h5, h6"

	titlePrefix        = "【記事タイトル】 "
	tableCaptionPrefix = "【表題】 "
)

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
	pageTitle := strings.TrimSpace(doc.Find("title").First().Text())
	if pageTitle != "" {
		parts = append(parts, titlePrefix+pageTitle)
	}

	// ノイズは本文候補を決める前にページ全体から落とします。あとから落とすと、
	// <aside> の中の <article> を本文として選んでしまう余地が残ります。
	doc.Find(noiseSelectors).Remove()

	// ブロック要素は DOM の出現順（深さ優先）に一度ずつ訪問されます。入れ子の
	// 親子が両方一致した場合は両方が訪問されるため、二重に出さない責任は
	// processGeneralElement 側にあります。
	findMainContent(doc).Find(blockSelectors).Each(func(_ int, s *goquery.Selection) {
		var content string

		switch {
		case s.Is("table"):
			content = processTable(s)
		case s.Is("pre"):
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
	if mainContent := doc.Find(mainContentSelectors).First(); mainContent.Length() > 0 {
		return mainContent
	}

	// 本文候補が無いページはページ全体を本文として扱い、囲み要素を DOM から取り除きます。
	// goquery の Not は「選択中のノード自身」を絞り込むだけで子孫には効かないため、
	// 以前の doc.Not(...) はドキュメントノードをそのまま返すだけで何も除外できておらず、
	// ナビゲーションやフッターのリンクが <li> として本文に混ざっていました。
	body := doc.Find("body").First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	body.Find(pageFrameSelectors).Remove()

	return body
}

// processGeneralElement は一般的なテキスト要素からテキストを抽出し、整形します。
func processGeneralElement(s *goquery.Selection) string {
	content := text.NormalizeText(ownText(s))
	if content == "" {
		return ""
	}

	if s.Is(headingSelectors) {
		if utf8.RuneCountInString(content) >= MinHeadingLength {
			return "## " + content
		}
		return ""
	}

	// リスト項目は短くても項目として意味を持つため、長さで落としません。
	if s.Is("li") || utf8.RuneCountInString(content) >= MinParagraphLength {
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

	var walk func(sel *goquery.Selection)
	walk = func(sel *goquery.Selection) {
		sel.Contents().Each(func(_ int, child *goquery.Selection) {
			node := child.Get(0)
			if node == nil {
				return
			}

			switch node.Type {
			case html.TextNode:
				builder.WriteString(node.Data)
			case html.ElementNode:
				if child.Is(blockSelectors) {
					return
				}
				walk(child)
			}
			// コメントノードやDOCTYPEなどは無視
		})
	}
	walk(s)

	return builder.String()
}

// processTable は goquery.Selection からテーブルの内容を抽出し、整形します。
func processTable(s *goquery.Selection) string {
	var tableContent []string
	captionText := strings.TrimSpace(s.Find("caption").First().Text())
	if captionText != "" {
		tableContent = append(tableContent, tableCaptionPrefix+captionText)
	}
	s.Find("tr").Each(func(_ int, row *goquery.Selection) {
		var rowTexts []string
		hasValue := false
		row.Find("th, td").Each(func(_ int, cell *goquery.Selection) {
			cellText := text.NormalizeText(cell.Text())
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
