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

// insideTable は n が table 要素の内側にあるかを返します。
//
// 表の中のブロック要素（<td><p>…</p></td> など）は、行の一部として processTable が
// 出力します。走査で改めて訪問すると同じ文が表の外にもう一度出るため、
// ブロック走査の側でこの判定を使って飛ばします。入れ子の表も同様で、外側の表が
// セルの文字列として平坦化して出します。
func insideTable(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "table" {
			return true
		}
	}
	return false
}

// processTable は表の内容を「セル | セル」の行に整形します。
//
// 行とセルは html.Node を直接辿って表の直下からだけ取ります。セレクタで tr / td を
// 探すと入れ子の表の行やセルまで外側の行に混ざり、同じセルが複数回出ます。
func processTable(s *goquery.Selection) string {
	table := s.Get(0)
	if table == nil {
		return ""
	}

	var tableContent []string
	if caption := childElement(table, "caption"); caption != nil {
		if captionText := normalizeSpace(allText(caption)); captionText != "" {
			tableContent = append(tableContent, tableCaptionPrefix+captionText)
		}
	}
	forEachRow(table, func(row *html.Node) {
		var rowTexts []string
		hasValue := false
		for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
				continue
			}
			cellText := normalizeSpace(allText(cell))
			if cellText != "" {
				hasValue = true
			}
			rowTexts = append(rowTexts, cellText)
		}
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

// forEachRow は table 直下の tr を文書順に fn へ渡します。
// thead / tbody / tfoot を挟んだ tr も含めますが、入れ子の表の tr は含めません。
func forEachRow(table *html.Node, fn func(row *html.Node)) {
	for child := table.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		switch child.Data {
		case "tr":
			fn(child)
		case "thead", "tbody", "tfoot":
			for row := child.FirstChild; row != nil; row = row.NextSibling {
				if row.Type == html.ElementNode && row.Data == "tr" {
					fn(row)
				}
			}
		}
	}
}

// childElement は n の直下にある最初の tag 要素を返します。無ければ nil です。
func childElement(n *html.Node, tag string) *html.Node {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			return child
		}
	}
	return nil
}

// allText は n の子孫のテキストをすべて連結します。表のセル用です。
//
// ownText と違ってブロック要素の内側にも立ち入りますが、ブロック要素・表の構造要素の
// 境界と <br> には空白を挟みます。挟まないと <td><ul><li>A</li><li>B</li></ul></td> が
// "AB" に融合します。連続した空白は呼び出し側の normalizeSpace がまとめます。
func allText(n *html.Node) string {
	var builder strings.Builder
	writeAllText(&builder, n)
	return builder.String()
}

func writeAllText(builder *strings.Builder, n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			builder.WriteString(child.Data)
		case html.ElementNode:
			if child.Data == "br" {
				builder.WriteByte(' ')
				continue
			}
			_, isBoundary := flattenBoundarySet[child.Data]
			if isBoundary {
				builder.WriteByte(' ')
			}
			writeAllText(builder, child)
			if isBoundary {
				builder.WriteByte(' ')
			}
		}
	}
}
