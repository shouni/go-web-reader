package extract_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/shouni/go-web-reader/extract"
)

func ExampleText() {
	html := `<html><head><title>Example</title></head><body>
<nav><a href="/">Home</a></nav>
<article>
  <h1>Heading</h1>
  <p>This paragraph is long enough to be treated as body text by the extractor.</p>
</article>
</body></html>`

	text, hasBody, err := extract.Text(context.Background(), strings.NewReader(html))
	if err != nil {
		panic(err)
	}
	fmt.Println(hasBody)
	fmt.Println(text)
	// Output:
	// true
	// 【記事タイトル】 Example
	//
	// ## Heading
	//
	// This paragraph is long enough to be treated as body text by the extractor.
}
