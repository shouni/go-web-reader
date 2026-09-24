package extract_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

// FuzzText は、任意のバイト列を HTML として渡しても落ちず、戻り値の組み合わせが
// 契約どおりであることを確かめます。
//
// 入力は利用者が指定した URL の中身、つまり完全な外部入力です。抽出は DOM を歩いて
// ブロック要素と箱要素の直下テキストを拾う走査で、深い入れ子・閉じていないタグ・
// 不正な UTF-8 のような形は表のテストでは並べきれません。
//
// 契約は 3 つです。エラーなら本文は空で hasBody は偽。hasBody が真なら本文は空でない。
// そして同じ入力からは同じ結果が出る（走査に map の反復順が漏れていない）。
func FuzzText(f *testing.F) {
	for _, s := range fuzzHTMLSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, html string) {
		text, hasBody, err := extract.Text(context.Background(), strings.NewReader(html))

		if err != nil {
			if text != "" || hasBody {
				t.Fatalf("Text() failed but returned (%q, %v)", text, hasBody)
			}
			return
		}
		if hasBody && strings.TrimSpace(text) == "" {
			t.Fatalf("Text() reported a body but returned only whitespace: %q", text)
		}

		// 同じ入力からは同じ結果。抽出は map を歩く箇所があるので、順序が漏れると
		// 通知や要約の内容が呼び出しごとに変わります。
		again, againHasBody, againErr := extract.Text(context.Background(), strings.NewReader(html))
		if againErr != nil || again != text || againHasBody != hasBody {
			t.Fatalf("Text() is not deterministic:\nfirst:  (%q, %v, %v)\nsecond: (%q, %v, %v)",
				text, hasBody, err, again, againHasBody, againErr)
		}
	})
}

// fuzzHTMLSeeds は、抽出が実際に当たる形を並べたものです。
func fuzzHTMLSeeds() []string {
	const long = "これは本文として採用されるだけの長さを持った文章です。"
	return []string{
		`<html><head><title>T</title></head><body><main><p>` + long + `</p></main></body></html>`,
		`<article><div class="entry">` + long + `<br><br>` + long + `</div></article>`,
		`<table><caption>C</caption><tr><th>h</th><td>d</td></tr></table>`,
		"<pre>  indented\n  code\n</pre>",
		`<ul><li>a</li><li>b</li></ul>`,
		`<p>` + long + `<p>` + long,                             // 閉じていない段落
		strings.Repeat("<div>", 200) + long,                     // 深い入れ子・閉じていない
		`<aside><article><p>` + long + `</p></article></aside>`, // ノイズの中の本文
		`<body><header>h</header><footer>f</footer></body>`,
		"<title>\x00\xff</title><p>\xd0\xd0</p>",
		"",
		"plain text, no tags at all",
	}
}
