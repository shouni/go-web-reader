package extract_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/extract"
)

// FuzzText checks that passing arbitrary bytes as HTML never crashes and that the combination of
// return values honours the contract.
//
// The input is the content behind a user-supplied URL, that is, fully external input. Extraction is
// a DOM walk that picks up the direct text of block and container elements, and shapes such as deep
// nesting, unclosed tags and invalid UTF-8 cannot all be listed in table tests.
//
// The contract has three parts. On error the text is empty and hasBody is false. When hasBody is
// true the text is not empty. And the same input gives the same result (no map iteration order
// leaks into the walk).
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

		// Same input, same result. Extraction walks maps in places, so an order leak would change the
		// contents of notifications and summaries from call to call.
		again, againHasBody, againErr := extract.Text(context.Background(), strings.NewReader(html))
		if againErr != nil || again != text || againHasBody != hasBody {
			t.Fatalf("Text() is not deterministic:\nfirst:  (%q, %v, %v)\nsecond: (%q, %v, %v)",
				text, hasBody, err, again, againHasBody, againErr)
		}
	})
}

// fuzzHTMLSeeds lists the shapes extraction actually meets.
func fuzzHTMLSeeds() []string {
	const long = "これは本文として採用されるだけの長さを持った文章です。"
	return []string{
		`<html><head><title>T</title></head><body><main><p>` + long + `</p></main></body></html>`,
		`<article><div class="entry">` + long + `<br><br>` + long + `</div></article>`,
		`<table><caption>C</caption><tr><th>h</th><td>d</td></tr></table>`,
		"<pre>  indented\n  code\n</pre>",
		`<ul><li>a</li><li>b</li></ul>`,
		`<p>` + long + `<p>` + long,                             // unclosed paragraphs
		strings.Repeat("<div>", 200) + long,                     // deep nesting, unclosed
		`<aside><article><p>` + long + `</p></article></aside>`, // body inside noise
		`<body><header>h</header><footer>f</footer></body>`,
		"<title>\x00\xff</title><p>\xd0\xd0</p>",
		"",
		"plain text, no tags at all",
	}
}
