// go-web-reader は、HTTP/HTTPS や GCS/S3 の URI からコンテンツを読み込み、
// 抽出結果を標準出力に表示する CLI ツールです。
package main

import "github.com/shouni/go-web-reader/cmd"

// main は CLI アプリケーションを起動します。
func main() {
	cmd.Execute()
}
