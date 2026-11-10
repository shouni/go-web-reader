package extract_test

import (
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

// Shift_JIS bytes. Using x/text directly in the tests would add a dependency only the tests have, so
// the converted bytes are written out directly
// (obtained with `printf '…' | iconv -f UTF-8 -t SHIFT_JIS | xxd -p`).
const (
	// sjisTitle is 「日本語の記事タイトル」 in Shift_JIS.
	sjisTitle = "\x93\xfa\x96\x7b\x8c\xea\x82\xcc\x8b\x4c\x8e\x96\x83\x5e\x83\x43\x83\x67\x83\x8b"
	// sjisBody is 「これはテストです」 repeated 3 times, in Shift_JIS
	// (to exceed MinParagraphLength).
	sjisBody = "\x82\xb1\x82\xea\x82\xcd\x83\x65\x83\x58\x83\x67\x82\xc5\x82\xb7" +
		"\x82\xb1\x82\xea\x82\xcd\x83\x65\x83\x58\x83\x67\x82\xc5\x82\xb7" +
		"\x82\xb1\x82\xea\x82\xcd\x83\x65\x83\x58\x83\x67\x82\xc5\x82\xb7"

	wantTitle = "日本語の記事タイトル"
	wantBody  = "これはテストですこれはテストですこれはテストです"
)

// sjisDocument builds Shift_JIS HTML. When metaCharset is empty the document itself declares no
// charset (the HTTP header is the only clue).
func sjisDocument(metaCharset string) string {
	meta := ""
	if metaCharset != "" {
		meta = `<meta http-equiv="Content-Type" content="text/html; charset=` + metaCharset + `">`
	}
	return "<html><head>" + meta + "<title>" + sjisTitle + "</title></head>" +
		"<body><main><p>" + sjisBody + "</p></main></body></html>"
}

// A non-UTF-8 page must be read without garbling. goquery (x/net/html) assumes UTF-8 input, so
// without a conversion it breaks as it is.
func TestTextDecodesNonUTF8(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		contentType string
	}{
		{
			// a page whose only clue is <meta charset> (no Content-Type passed)
			name: "meta_charset_only",
			html: sjisDocument("Shift_JIS"),
		},
		{
			// a page that declares nothing, with the HTTP header as the only clue
			name:        "content_type_header_only",
			html:        sjisDocument(""),
			contentType: "text/html; charset=Shift_JIS",
		},
		{
			// The header wins. Even when meta is wrong, the header's declaration is followed.
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
				t.Errorf("title was not decoded\n got: %q\nwant contains: %q", text, wantTitle)
			}
			if !strings.Contains(text, wantBody) {
				t.Errorf("body was not decoded\n got: %q\nwant contains: %q", text, wantBody)
			}
		})
	}
}

// An undeclared UTF-8 page must not be misdetected as windows-1252 and broken.
// The default charset is windows-1252, so without UTF-8 auto-detection all Japanese is garbled.
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
		t.Errorf("UTF-8 page is broken: %q", text)
	}
}

// Engine must also provide the path with Content-Type (reader uses it).
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
		t.Errorf("title was not decoded: %q", text)
	}
}
