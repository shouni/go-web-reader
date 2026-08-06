package pipeline_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shouni/go-web-reader/internal/pipeline"
	"github.com/shouni/go-web-reader/pkg/reader"
)

// TestEndToEndHTTPToWriter は pkg/reader → internal/pipeline → io.Writer の経路を
// 実物で通します。層をまたいだ配線（抽出器の適用、ストリームの受け渡し、Close）は
// 単体テストのスタブでは確認できないため、ここだけ実際の HTTP サーバーを立てます。
func TestEndToEndHTTPToWriter(t *testing.T) {
	body := "<html><body><article><h1>T</h1><p>" + strings.Repeat("本文テスト。", 40) + "</p></article></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// 既定の HTTP クライアントも securenet でループバックを遮断するため、
	// テスト用サーバーへ届くよう素のクライアントを注入する。
	r, err := reader.New(
		reader.WithSafeURLValidator(func(context.Context, string) error { return nil }),
		reader.WithHTTPClient(&http.Client{}),
	)
	if err != nil {
		t.Fatalf("reader.New() error = %v", err)
	}
	defer func() { _ = r.Close() }()

	p, err := pipeline.NewPipeline(srv.URL+"/a.html", r)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	var out bytes.Buffer
	if err := p.Execute(context.Background(), &out); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "本文テスト。") {
		t.Fatalf("抽出結果に本文が含まれていない: %q", out.String())
	}
	if strings.Contains(out.String(), "<html>") {
		t.Fatal("HTML が抽出されずそのまま流れている")
	}
}
