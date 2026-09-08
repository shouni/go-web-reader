package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// insideTable は n が table 要素の内側にあるかを返します。
// 表の中のブロック要素は processTable が行の一部として出すので、ブロック走査は飛ばします。
func insideTable(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "table" {
			return true
		}
	}
	return false
}

// processTable は表の内容を「セル | セル」の行に整形します。
// 行とセルは表の直下からだけ取ります。セレクタで tr / td を探すと入れ子の表の分まで外側の行に混ざります。
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
		// セルが無い行や全セルが空の行は空行にしかならない。
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

// allText は n の子孫のテキストをすべて連結します（表のセル用）。ownText と違って
// ブロック要素にも立ち入りますが、その境界と <br> には空白を挟みます。挟まないと
// <li>A</li><li>B</li> が "AB" に融合します。
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
