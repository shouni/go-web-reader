package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// insideTable reports whether n is inside a table element.
// processTable emits the block elements inside a table as part of a row, so the block scan skips them.
func insideTable(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "table" {
			return true
		}
	}
	return false
}

// processTable formats a table's contents as "cell | cell" rows.
// Rows and cells are taken only from directly under the table. Finding tr / td with a selector would mix a
// nested table's rows into the outer rows.
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
		// A row with no cells, or with every cell empty, would only become a blank line.
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

// forEachRow passes the tr elements directly under table to fn in document order.
// It includes tr elements under thead / tbody / tfoot, but not those of a nested table.
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

// childElement returns the first tag element directly under n, or nil if there is none.
func childElement(n *html.Node, tag string) *html.Node {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			return child
		}
	}
	return nil
}

// allText concatenates all text under n (for table cells). Unlike ownText it descends into block
// elements, but puts a space at their boundaries and at <br>. Without it, <li>A</li><li>B</li> would
// fuse into "AB".
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
