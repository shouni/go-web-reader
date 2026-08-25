package extract_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

// realisticDoc は記事ページを模した HTML を生成します。
func realisticDoc(sections int) string {
	var b strings.Builder
	b.WriteString(`<html><head><title>Benchmark Article</title></head><body>
	  <nav><ul><li><a href="/">Home</a></li><li><a href="/a">About</a></li></ul></nav>
	  <main><article>`)
	for i := range sections {
		fmt.Fprintf(&b, `
		  <h2>Section heading number %d</h2>
		  <p>This is a <strong>paragraph</strong> with <a href="#">inline markup</a> and
		     <em>emphasis</em>, long enough to be treated as article body text.</p>
		  <ul><li>List item <code>one</code></li><li>List item two</li></ul>
		  <table><tr><th>Key</th><td>Value %d</td></tr></table>
		  <pre>code block %d</pre>`, i, i, i)
	}
	b.WriteString(`</article></main>
	  <footer><ul><li>Privacy</li><li>Terms</li></ul></footer></body></html>`)
	return b.String()
}

func BenchmarkText(b *testing.B) {
	for _, sections := range []int{10, 100} {
		doc := realisticDoc(sections)
		b.Run(fmt.Sprintf("sections=%d", sections), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := extract.Text(b.Context(), strings.NewReader(doc)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTextCharset は文字コード判定の経路ごとのコストを測ります。
//
// 宣言の無い UTF-8（高位ビットあり）だけが変換なしで済み、それ以外は
// transform.Reader を 1 枚挟みます。ASCII だけの文書は判定材料が無いため
// 既定の windows-1252 として復号されます（ASCII の範囲では恒等変換）。
func BenchmarkTextCharset(b *testing.B) {
	ascii := realisticDoc(100)
	japanese := strings.ReplaceAll(ascii, "This is a", "これは日本語の")

	cases := []struct {
		name        string
		doc         string
		contentType string
	}{
		{name: "ascii-undeclared", doc: ascii},
		{name: "utf8-undeclared", doc: japanese},
		{name: "utf8-declared", doc: japanese, contentType: "text/html; charset=utf-8"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := extract.TextWithContentType(b.Context(), strings.NewReader(tc.doc), tc.contentType); err != nil {
					b.Fatalf("TextWithContentType() error = %v", err)
				}
			}
		})
	}
}
