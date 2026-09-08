package reader_test

import (
	"context"
	"fmt"

	"github.com/shouni/go-web-reader/reader"
)

// ネットワークに出るため Output は付けていません（コンパイルだけ行われます）。
func ExampleUniversalReader_ReadAll() {
	r := reader.New()
	defer func() { _ = r.Close() }()

	body, err := r.ReadAll(context.Background(), "https://example.com/article")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body))
}
