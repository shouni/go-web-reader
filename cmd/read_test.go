package cmd

import (
	"bytes"
	"testing"
)

// 出力があったときにだけ枠を出すこと。取得前に見出しを出すと、
// 失敗時に「--- 取得結果 ---」だけが残ります。
func TestDecoratedWriterEmitsHeaderOnlyWithOutput(t *testing.T) {
	t.Parallel()

	t.Run("本文があれば見出しと区切り線が出る", func(t *testing.T) {
		var out, notice bytes.Buffer
		w := &decoratedWriter{out: &out, notice: &notice, header: "HEAD"}

		if _, err := w.Write([]byte("body")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		w.finish("FOOT")

		if got := out.String(); got != "body" {
			t.Fatalf("stdout = %q, want %q (装飾が混ざってはいけない)", got, "body")
		}
		if got := notice.String(); got != "HEAD\n\nFOOT\n" {
			t.Fatalf("stderr = %q", got)
		}
	})

	t.Run("本文が無ければ何も出ない", func(t *testing.T) {
		var out, notice bytes.Buffer
		w := &decoratedWriter{out: &out, notice: &notice, header: "HEAD"}

		w.finish("FOOT")

		if notice.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", notice.String())
		}
	})

	t.Run("空の書き込みでは見出しを出さない", func(t *testing.T) {
		var out, notice bytes.Buffer
		w := &decoratedWriter{out: &out, notice: &notice, header: "HEAD"}

		if _, err := w.Write(nil); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if notice.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", notice.String())
		}
	})
}
