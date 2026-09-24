package reader

import (
	"strings"
	"testing"
)

// FuzzResolveMediaType は、Content-Type ヘッダーの解釈が落ちず、戻り値の組み合わせが
// 契約どおりであることを確かめます。
//
// 値はサーバーが返すものなので、壊れた形はそのまま届きます（パラメータの引用符が
// 閉じていない、charset が空、区切りだけ、など）。標準の mime.ParseMediaType が
// 拒んだ場合に自前で前半を切り出す経路があり、そこが表のテストの手薄なところです。
//
// 契約は 2 つです。エラーなら結果は空。成功したなら、その結果は小文字で、
// パラメータ（; 以降）も前後の空白も残っていない。
func FuzzResolveMediaType(f *testing.F) {
	seeds := []string{
		"text/html; charset=utf-8",
		"TEXT/HTML",
		"  text/plain  ",
		"application/json;charset=Shift_JIS",
		`text/html; charset="utf-8`,
		"image/png",
		"text/html;",
		";",
		"/",
		"",
		"text/html; charset=utf-8; boundary=x",
		"application/octet-stream",
		"\x00",
		strings.Repeat("a", 500) + "/b",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, contentType string) {
		got, err := resolveMediaType(contentType)

		if err != nil {
			if got != "" {
				t.Fatalf("resolveMediaType(%q) failed but returned %q", contentType, got)
			}
			return
		}
		if got == "" {
			return // 空のヘッダーは「不明」として空を返す。
		}
		if got != strings.ToLower(got) {
			t.Fatalf("resolveMediaType(%q) = %q, want it lowercased", contentType, got)
		}
		if strings.ContainsAny(got, "; ") || strings.TrimSpace(got) != got {
			t.Fatalf("resolveMediaType(%q) = %q, want no parameters or padding", contentType, got)
		}
	})
}
