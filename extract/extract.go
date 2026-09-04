// Package extract は、HTMLコンテンツから本文テキストを高精度に抽出します。
//
// 取得（HTTP アクセス）はこのパッケージの責務ではありません。呼び出し側が
// 用意した io.Reader を受け取って解析するだけなので、HTTP でもファイルでも
// テスト用の文字列でも同じ経路で扱えます。
//
// 入力は UTF-8 でなくても構いません。BOM・<meta charset>・本文のバイト列から
// 文字コードを判定して UTF-8 に変換してから解析します。Content-Type ヘッダーが
// 手元にある場合は TextWithContentType に渡してください。
//
// # 本文抽出のルール
//
// 「記事本文だけを残す」ことを目的にしたヒューリスティックで、次の順に処理します。
//
// 0. 文字コードを判定する。BOM → Content-Type の charset → <meta charset> →
// 本文のバイト列、の順に判定して UTF-8 に変換します。変換を挟まないと、パーサが
// 入力を UTF-8 とみなすため非 UTF-8 のページがそのまま文字化けします。
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
// 一度だけ出力されます。<br> は空白として扱うため、前後の行が 1 語に融合しません。
// 出力の形は次のとおりです。
//
//   - title — 「【記事タイトル】 」を付けて先頭に
//   - h1〜h6 — 「## 」を付ける（MinHeadingLength 文字以上のもの）
//   - p, blockquote — MinParagraphLength 文字以上のものだけ
//   - li, dt, dd, figcaption — 長さを問わず出力（項目・定義語・キャプションは
//     短くても意味を持つため）
//   - table — 「【表題】 」付きキャプションと「セル | セル」の行（空行は出力しない）
//   - pre — コードフェンスで囲む
//
// しきい値はバイト数ではなく文字数で測ります。len() で測ると日本語は 1 文字 3 バイト
// ぶんなのでしきい値が実質 1/3 になり、ナビゲーションの断片が本文として残ります。
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
//
// 中身は Text と同じで、抽出エンジンを差し替えられる形（インターフェース）で
// 受け取りたい呼び出し側のために型として公開しています。単に一度抽出したいだけなら
// Text をそのまま呼んでください。
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
// HTML の文字コードは HTTP ヘッダーの charset が最優先で、<meta charset> は
// その次です。ヘッダーが手元にあるならこちらを使ってください。charset を
// 宣言しない Shift_JIS / EUC-JP のページは、ヘッダーを渡さないと判定材料が
// なくなり文字化けします。contentType が空の場合の挙動は Text と同じです。
func TextWithContentType(ctx context.Context, r io.Reader, contentType string) (text string, hasBodyFound bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	// goquery（golang.org/x/net/html）は入力を UTF-8 とみなすため、
	// 変換を挟まないと非 UTF-8 のページがそのまま文字化けします。
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

	// タイトルは <head> にあるため、本文の絞り込みより先に取ります。
	pageTitle := normalizeSpace(doc.FindMatcher(titleMatcher).First().Text())
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
		if content := processBlock(s); content != "" {
			parts = append(parts, content)
		}
	})

	return validateAndFormatResult(parts)
}

// processBlock はブロック要素 1 つ分の出力を返します。出力しない場合は空文字列です。
func processBlock(s *goquery.Selection) string {
	switch tagName(s) {
	case "table":
		return processTable(s)
	case "pre":
		// pre タグ (コードブロック) はコードフェンスで囲む。
		// 整形済みテキストなので normalizeSpace は通さず、字下げを保ちます。
		preText := strings.TrimSpace(s.Text())
		if preText == "" {
			return ""
		}
		return "```\n" + preText + "\n```"
	default:
		// 一般的なテキスト要素 (p, h*, li, dt, dd, figcaption, blockquote)
		return processGeneralElement(s)
	}
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
