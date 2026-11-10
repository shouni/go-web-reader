package reader

import (
	"strings"
	"testing"
)

// FuzzResolveMediaType checks that interpreting a Content-Type header never crashes and that the
// combination of return values honours the contract.
//
// The value comes from the server, so broken shapes arrive as they are (a parameter's quote left
// unclosed, an empty charset, nothing but separators, and so on). When the standard
// mime.ParseMediaType rejects a value there is a hand-rolled path that cuts out the first half, and
// that is where the table tests are thin.
//
// The contract has two parts. On error the result is empty. On success the result is lower case,
// with no parameters (anything after ;) and no surrounding whitespace left.
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
			return // An empty header means "unknown" and returns empty.
		}
		if got != strings.ToLower(got) {
			t.Fatalf("resolveMediaType(%q) = %q, want it lowercased", contentType, got)
		}
		if strings.ContainsAny(got, "; ") || strings.TrimSpace(got) != got {
			t.Fatalf("resolveMediaType(%q) = %q, want no parameters or padding", contentType, got)
		}
	})
}
