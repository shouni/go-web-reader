package extract

import (
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

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

	// リスト項目や定義語は短くても項目として意味を持つため、長さで落としません。
	if _, isShort := shortTagSet[tag]; isShort {
		return content
	}
	if utf8.RuneCountInString(content) >= MinParagraphLength {
		return content
	}
	return ""
}

// ownText は s 配下のテキストのうち、s 自身が担当する分だけを連結します。
//
// 子孫のブロック要素（blockTags）は走査対象として別途訪問されるため、ここでは
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
			// <br> は改行そのものなので、区切りを入れないと前後の行が
			// 1 語に融合します（"line1<br>line2" → "line1line2"）。
			// 後段の normalizeSpace が連続空白をまとめるため、空白 1 個で足ります。
			if child.Data == "br" {
				builder.WriteByte(' ')
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
