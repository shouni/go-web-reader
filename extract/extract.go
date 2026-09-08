// Package extract は、HTMLコンテンツから本文テキストを高精度に抽出します。
//
// 取得（HTTP アクセス）はこのパッケージの責務ではありません。呼び出し側が用意した
// io.Reader を解析するだけなので、HTTP でもファイルでもテスト用の文字列でも同じ経路です。
//
// # 本文抽出のルール
//
// 「記事本文だけを残す」ことを目的にしたヒューリスティックで、次の順に処理します。
//
// 0. 文字コードを判定する。BOM → Content-Type の charset → <meta charset> →
// 本文のバイト列、の順に判定して UTF-8 に変換します。パーサは入力を UTF-8 とみなすため、
// これが無いと非 UTF-8 のページが丸ごと文字化けします。Content-Type ヘッダーが
// 手元にあるなら TextWithContentType に渡してください。
//
// 1. ノイズを落とす。script、style、form、nav、aside、noscript、template、
// [hidden]、[aria-hidden="true"]、および広告・SNS・コメント欄まわりのクラス
// （.related-posts、.social-share、.comments、.ad-banner、.advertisement）を
// ページ全体から除去します。noscript / template はパーサからは中身がただの
// テキストに見えるため、落とさないと囲っている段落の本文に混ざります。
//
// 2. 本文の範囲を決める。article、main、div[role='main']、#main、#content、
// .post-content、.article-body、.entry-content、.markdown-body、.readme に
// 最初に一致した要素を本文とします。見つからない場合はページ全体を本文とみなし、
// そのときだけ header / footer / .sidebar も落とします（記事の内側の header は
// 見出しを、footer は署名を含むことがあるため、常に落とすと本文が欠けます）。
//
// 3. ブロック要素を順に拾う。p、h1〜h6、li、dt、dd、figcaption、blockquote、
// table、pre を DOM の出現順に走査します。入れ子（<li><p>…</p></li> など）は
// 一度だけ出力され、表の中身は表の行としてだけ出ます。<br> は空白として扱います。
// 出力の形は次のとおりです。
//
//   - title — 「【記事タイトル】 」を付けて先頭に
//   - h1〜h6 — 「## 」を付ける（MinHeadingLength 文字以上のもの）
//   - p, blockquote — MinParagraphLength 文字以上のものだけ
//   - li, dt, dd, figcaption — 長さを問わず出力（短くても項目として意味を持つため）
//   - table — 「【表題】 」付きキャプションと「セル | セル」の行。セルの中は
//     長さを問わず平坦化し、空の行は出力しない
//   - pre — コードフェンスで囲む
//
// しきい値はバイト数ではなく文字数で測ります。len() だと日本語は 1 文字 3 バイトで
// しきい値が実質 1/3 になり、ナビゲーションの断片が本文として残ります。
package extract

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
)

// Engine は状態を持たない抽出エンジンです。ゼロ値のまま使えます。
// 中身は Text と同じで、インターフェース値として渡したい呼び出し側のための型です。
type Engine struct{}

// Extract は取得済みのHTMLコンテンツから整形されたテキストを抽出します。
func (Engine) Extract(ctx context.Context, r io.Reader) (string, bool, error) {
	return Text(ctx, r)
}

// ExtractWithContentType は Content-Type ヘッダーを添えて Extract します。
// 文字コードの判定にヘッダーの charset を使える分だけ Extract より正確です。
func (Engine) ExtractWithContentType(ctx context.Context, r io.Reader, contentType string) (string, bool, error) {
	return TextWithContentType(ctx, r, contentType)
}

// Text は取得済みのHTMLコンテンツから整形されたテキストを抽出します。
//
// 第2戻り値は本文が見つかったかどうかです。タイトルしか取れなかった場合は
// テキストを返しつつ false になります（エラーではありません）。
func Text(ctx context.Context, r io.Reader) (text string, hasBodyFound bool, err error) {
	return TextWithContentType(ctx, r, "")
}

// TextWithContentType は、Content-Type ヘッダーの値を添えて Text します。
//
// 文字コードはヘッダーの charset が <meta charset> より優先されます。<meta> で
// 宣言しない Shift_JIS / EUC-JP のページはヘッダーだけが手がかりなので、手元に
// あるなら渡してください。contentType が空なら Text と同じです。
func TextWithContentType(ctx context.Context, r io.Reader, contentType string) (text string, hasBodyFound bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	// パーサは入力を UTF-8 とみなすため、先に変換する。
	decoded, err := charset.NewReader(r, contentType)
	if err != nil {
		return "", false, fmt.Errorf("文字コードの判定に失敗しました: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(decoded)
	if err != nil {
		return "", false, fmt.Errorf("HTML解析に失敗しました: %w", err)
	}

	return extractContentText(doc)
}

// extractContentText はgoquery.Documentから本文とタイトルを抽出し、整形します。
func extractContentText(doc *goquery.Document) (text string, hasBodyFound bool, err error) {
	var parts []string

	// タイトルは <head> にあるため、本文の絞り込みより先に取る。
	pageTitle := normalizeSpace(doc.FindMatcher(titleMatcher).First().Text())
	if pageTitle != "" {
		parts = append(parts, titlePrefix+pageTitle)
	}

	// ノイズは本文候補を決める前にページ全体から落とす。あとから落とすと
	// <aside> の中の <article> を本文に選ぶ余地が残る。
	doc.FindMatcher(noiseMatcher).Remove()

	// 入れ子のブロックは親子とも訪問される。二重に出さないのは ownText の役目で、
	// 表の中身だけは表が行として出すので、ここで飛ばす。
	findMainContent(doc).FindMatcher(blockMatcher).Each(func(_ int, s *goquery.Selection) {
		if insideTable(s.Get(0)) {
			return
		}
		if content := processBlock(s); content != "" {
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

	// 本文候補が無ければページ全体を本文とし、囲み要素を DOM から取り除く。
	// goquery の Not は選択中のノード自身しか絞り込まず子孫に効かないので、Remove で消す。
	body := doc.FindMatcher(bodyMatcher).First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	body.FindMatcher(pageFrameMatcher).Remove()

	return body
}

// processBlock はブロック要素 1 つ分の出力を返します。出力しない場合は空文字列です。
func processBlock(s *goquery.Selection) string {
	switch tagName(s) {
	case "table":
		return processTable(s)
	case "pre":
		// 整形済みテキストなので normalizeSpace は通さず、字下げを保つ。
		preText := strings.TrimSpace(s.Text())
		if preText == "" {
			return ""
		}
		return "```\n" + preText + "\n```"
	default:
		return processGeneralElement(s)
	}
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
