package reader_test

import (
	"context"
	"fmt"

	"github.com/shouni/go-web-reader/reader"
)

// It reaches the network, so it has no Output comment (it is only compiled).
func ExampleUniversalReader_ReadAll() {
	r := reader.New()
	defer func() { _ = r.Close() }()

	body, err := r.ReadAll(context.Background(), "https://example.com/article")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body))
}
