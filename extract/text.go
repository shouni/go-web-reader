package extract

import (
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// processGeneralElement extracts and formats the text of an ordinary text element.
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

	if _, isShort := shortTagSet[tag]; isShort {
		return content
	}
	if utf8.RuneCountInString(content) >= MinParagraphLength {
		return content
	}
	return ""
}

// ownText concatenates only the part of the text under s that s itself is responsible for.
// Descendant block elements are visited separately, so it does not descend into them; if it did,
// <li><p>…</p></li> would have parent and child emit the same sentence.
func ownText(s *goquery.Selection) string {
	var builder strings.Builder
	for _, node := range s.Nodes {
		writeOwnText(&builder, node)
	}
	return builder.String()
}

// writeOwnText writes the text under n to builder, excluding the inside of block elements.
// goquery's Contents().Each allocates a Selection per child node, so it walks html.Node directly.
func writeOwnText(builder *strings.Builder, n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			builder.WriteString(child.Data)
		case html.ElementNode:
			if _, isBlock := blockTagSet[child.Data]; isBlock {
				continue
			}
			// Unless <br> becomes a space, the lines on either side fuse into one word ("line1<br>line2" → "line1line2").
			if child.Data == "br" {
				builder.WriteByte(' ')
				continue
			}
			writeOwnText(builder, child)
		}
	}
}

// writeTextWithBreaks writes the text under n to builder, turning <br> into a newline.
// It is for bodies written directly in container elements, where <br> serves as the paragraph break.
func writeTextWithBreaks(builder *strings.Builder, n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			builder.WriteString(child.Data)
		case html.ElementNode:
			if child.Data == "br" {
				builder.WriteByte('\n')
				continue
			}
			writeTextWithBreaks(builder, child)
		}
	}
}

// normalizeSpace collapses runs of whitespace (newlines and tabs included) into one space and trims both ends.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
