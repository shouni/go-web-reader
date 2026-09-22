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

	if _, isShort := shortTagSet[tag]; isShort {
		return content
	}
	if utf8.RuneCountInString(content) >= MinParagraphLength {
		return content
	}
	return ""
}

// ownText は s 配下のテキストのうち、s 自身が担当する分だけを連結します。
// 子孫のブロック要素は別途訪問されるので立ち入りません。立ち入ると
// <li><p>…</p></li> で親と子が同じ文を出します。
func ownText(s *goquery.Selection) string {
	var builder strings.Builder
	for _, node := range s.Nodes {
		writeOwnText(&builder, node)
	}
	return builder.String()
}

// writeOwnText は n の子孫のテキストを、ブロック要素の内側を除いて builder に書き出します。
// goquery の Contents().Each は子ノードごとに Selection を確保するため、html.Node を直接辿ります。
func writeOwnText(builder *strings.Builder, n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			builder.WriteString(child.Data)
		case html.ElementNode:
			if _, isBlock := blockTagSet[child.Data]; isBlock {
				continue
			}
			// <br> を空白にしないと前後の行が 1 語に融合する（"line1<br>line2" → "line1line2"）。
			if child.Data == "br" {
				builder.WriteByte(' ')
				continue
			}
			writeOwnText(builder, child)
		}
	}
}

// writeTextWithBreaks は n の子孫のテキストを builder に書き出し、<br> を改行にします。
// 箱要素の直書き本文で、<br> が段落の区切りとして使われているのを拾うためです。
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

// normalizeSpace は連続する空白（改行やタブを含む）を 1 個のスペースにまとめ、前後の空白を落とします。
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
