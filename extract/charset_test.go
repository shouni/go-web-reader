package extract_test

import (
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

// Shift_JIS のバイト列。テスト用に x/text を直接使うと、本体が持たない依存が
// テストにだけ増えるため、変換済みのバイト列を直接置いています。
// （`printf '…' | iconv -f UTF-8 -t SHIFT_JIS | xxd -p` で得られます）
const (
	// sjisTitle は「日本語の記事タイトル」の Shift_JIS 表現です。
	sjisTitle = "\x93\xfa\x96\x7b\x8c\xea\x82\xcc\x8b\x4c\x8e\x96\x83\x5e\x83\x43\x83\x67\x83\x8b"
	// sjisBody は「これはテストです」を 3 回繰り返した Shift_JIS 表現です
	// （MinParagraphLength を超えさせるため）。
	sjisBody = "\x82\xb1\x82\xea\x82\xcd\x83\x65\x83\x58\x83\x67\x82\xc5\x82\xb7" +
		"\x82\xb1\x82\xea\x82\xcd\x83\x65\x83\x58\x83\x67\x82\xc5\x82\xb7" +
		"\x82\xb1\x82\xea\x82\xcd\x83\x65\x83\x58\x83\x67\x82\xc5\x82\xb7"

	wantTitle = "日本語の記事タイトル"
	wantBody  = "これはテストですこれはテストですこれはテストです"
)

// sjisDocument は Shift_JIS の HTML を組み立てます。metaCharset が空なら
// 文書自身は文字コードを名乗りません（HTTP ヘッダーだけが手がかりになります）。
func sjisDocument(metaCharset string) string {
	meta := ""
	if metaCharset != "" {
		meta = `<meta http-equiv="Content-Type" content="text/html; charset=` + metaCharset + `">`
	}
	return "<html><head>" + meta + "<title>" + sjisTitle + "</title></head>" +
		"<body><main><p>" + sjisBody + "</p></main></body></html>"
}

// 非 UTF-8 のページが文字化けせず読めること。goquery（x/net/html）は
// 入力を UTF-8 とみなすため、変換を挟まないとそのまま壊れます。
func TestTextDecodesNonUTF8(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		contentType string
	}{
		{
			// <meta charset> だけが手がかりのページ（Content-Type は渡さない）
			name: "meta_charset_only",
			html: sjisDocument("Shift_JIS"),
		},
		{
			// 文書は名乗らず、HTTP ヘッダーだけが手がかりのページ
			name:        "content_type_header_only",
			html:        sjisDocument(""),
			contentType: "text/html; charset=Shift_JIS",
		},
		{
			// ヘッダーが最優先。meta が誤っていてもヘッダーの宣言に従う。
			name:        "header_wins_over_meta",
			html:        sjisDocument("UTF-8"),
			contentType: "text/html; charset=Shift_JIS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, hasBody, err := extract.TextWithContentType(t.Context(), strings.NewReader(tt.html), tt.contentType)
			if err != nil {
				t.Fatalf("TextWithContentType() error = %v", err)
			}
			if !hasBody {
				t.Fatalf("hasBodyFound = false, want true (text: %q)", text)
			}
			if !strings.Contains(text, wantTitle) {
				t.Errorf("タイトルが復号されていません\n got: %q\nwant contains: %q", text, wantTitle)
			}
			if !strings.Contains(text, wantBody) {
				t.Errorf("本文が復号されていません\n got: %q\nwant contains: %q", text, wantBody)
			}
		})
	}
}

// 宣言の無い UTF-8 ページを windows-1252 と誤判定して壊さないこと。
// 既定の文字コードは windows-1252 なので、UTF-8 の自動判定が効かないと
// 日本語がすべて化けます。
func TestTextKeepsUndeclaredUTF8(t *testing.T) {
	html := `<html><head><title>宣言なしの日本語ページ</title></head>` +
		`<body><main><p>` + wantBody + `</p></main></body></html>`

	text, hasBody, err := extract.Text(t.Context(), strings.NewReader(html))
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if !hasBody {
		t.Fatalf("hasBodyFound = false, want true (text: %q)", text)
	}
	if !strings.Contains(text, "宣言なしの日本語ページ") || !strings.Contains(text, wantBody) {
		t.Errorf("UTF-8 のページが壊れています: %q", text)
	}
}

// Engine は Content-Type つきの経路も提供すること（reader 側がこれを使います）。
func TestEngineExtractWithContentType(t *testing.T) {
	text, hasBody, err := extract.Engine{}.ExtractWithContentType(
		t.Context(),
		strings.NewReader(sjisDocument("")),
		"text/html; charset=Shift_JIS",
	)
	if err != nil {
		t.Fatalf("ExtractWithContentType() error = %v", err)
	}
	if !hasBody {
		t.Fatalf("hasBodyFound = false, want true (text: %q)", text)
	}
	if !strings.Contains(text, wantTitle) {
		t.Errorf("タイトルが復号されていません: %q", text)
	}
}
