// Package closeutil は、複数のクローズ処理をまとめて実行し、
// 発生したエラーを結合するための共通ヘルパーを提供します。
package closeutil

import "errors"

// Join は与えられたクローズ関数をすべて実行し、発生したエラーを errors.Join でまとめて返します。
// nil の関数はスキップされます。
func Join(closeFns ...func() error) error {
	var errs []error
	for _, fn := range closeFns {
		if fn == nil {
			continue
		}
		if err := fn(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
