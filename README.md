# 📖 Go Web Reader

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/shouni/go-web-reader)](https://goreportcard.com/report/github.com/shouni/go-web-reader)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)
[![Status](https://img.shields.io/badge/Status-Completed-brightgreen)](#)

## 🚀 概要 (About) — Web とクラウドストレージを扱うユニバーサル・リーダー

**Go Web Reader** は、Web サイトの本文抽出とクラウドストレージ（GCS/S3）の読み取りを、単一のインターフェースで扱う Go 言語向けライブラリです。

`https://`、`gs://`、`s3://` といった **URI** を渡すだけで、背後のアクセス手段の違いを意識せずにコンテンツを取得できます。
公開 API の中心は `pkg/reader` で、`reader.New()` でリーダーを生成し、`Open(ctx, uri)` 呼び出し時に URI スキームに応じて処理を切り替えます。

-----

## ✨ 提供機能 (Features)

### 🌐 [extract] ユニバーサル・コンテンツ抽出

* **Unified Interface**: URL が指定された場合、HTTP レスポンスの `Content-Type` に応じて HTML 抽出またはテキスト読み取りを切り替えます。
* **HTML Extraction**: `text/html` と `application/xhtml+xml` では Web 抽出エンジンが起動し、ノイズ（広告・ナビゲーション）を除去した「本文のみ」をストリームとして返します。
* **Plain Text / Markdown**: `text/plain`、`text/markdown`、`text/x-markdown` は変換せず、そのままストリームとして返します。
* **Safe URL Validation**: Web URL は取得前に安全性を検証し、検証エラーは呼び出し元で追跡できる形で返します。
* **Heuristic Engine**: DOM 構造を解析し、文脈を維持したまま高精度にメインテキストを特定します。

### ☁️ [remote] マルチプロトコル I/O

* **Storage Agnostic**: GCS と S3 を同じ `Open(ctx, uri)` インターフェースで扱えます。
* **Lazy Initialization**: GCS/S3 のクライアントは必要になった時だけ初期化され、`Close()` でまとめて解放されます。

### ⚡ [orchestration] 実行オーケストレーション

* **CLI Entry Point**: `cmd/read.go` から URI を受け取り、パイプライン経由で結果を標準出力に出力します。
* **Dependency Injection**: `internal/builder` で `reader` と `pipeline` を組み立て、`internal/pipeline` は抽象化された `ContentReader` に依存します。
* **Resource Safety**: アプリケーション終了時の close エラーは握りつぶさず、ログや呼び出し元で確認できるようにしています。

-----

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-web-reader/
├── cmd/                # CLI コマンド定義
│   ├── read.go         #   - 'read' サブコマンドの実行
│   └── root.go         #   - ルートコマンド・フラグ・初期化
├── pkg/
│   └── reader/         # 【PUBLIC】外部公開用エントリポイント
│       ├── reader.go   #   - ユニバーサル・リーダー本体
│       ├── options.go  #   - テストや組み込み向けの依存差し替えオプション
│       ├── http.go     #   - HTTP Content-Type 分岐と HTML 抽出
│       ├── storage.go  #   - GCS/S3 リーダーの遅延初期化とキャッシュ管理
│       └── reader_test.go # - スキーム分岐とリソース管理のテスト
└── internal/
    ├── app/            # アプリケーション層
    │   ├── container.go #  - 実行時コンテナとクローズ対象の管理
    │   └── container_test.go
    ├── builder/        # 依存関係の注入 (DI)
    │   └── app.go      #   - アプリケーション全体の構築
    ├── config/         # 設定管理
    │   └── config.go   #   - フラグ構造体の定義と検証
    ├── domain/         # ドメイン層
    │   └── ports.go    #   - 共通インターフェース・抽象定義
    └── pipeline/       # ビジネスロジック / 実行フロー
        ├── pipeline.go #   - ContentReader を使ったコンテンツ取得シーケンス
        └── pipeline_test.go
```

-----

## 🚦 使い方 (Usage)

### CLI

```bash
go run . read --uri https://example.com/article
go run . read --uri gs://bucket/path/to/file.txt
go run . read --uri s3://bucket/path/to/file.txt
```

### Library

```go
import (
    "context"
    "fmt"
    "io"

    "github.com/shouni/go-web-reader/pkg/reader"
)

func read(ctx context.Context, uri string) error {
    r, err := reader.New()
    if err != nil {
        return err
    }
    defer func() {
        _ = r.Close()
    }()

    stream, err := r.Open(ctx, uri)
    if err != nil {
        return err
    }
    defer func() {
        _ = stream.Close()
    }()

    body, err := io.ReadAll(stream)
    if err != nil {
        return err
    }
    fmt.Println(string(body))

    return nil
}
```

-----

## 🔧 実装メモ (Implementation Notes)

* `pkg/reader.New()` は軽量な初期化だけを行い、実際の GCS/S3 クライアント生成は `Open(ctx, uri)` の呼び出し時に遅延実行されます。
* HTTP(S) URI はまず `Content-Type` を判定し、HTML/XHTML は取得済みレスポンスボディから本文抽出、plain text/Markdown は生テキストのまま返します。その他の media type は未対応エラーになります。
* `pkg/reader` は GCS/S3 の reader と closer を内部キャッシュとして保持し、初回アクセス後は同じクライアントを再利用します。
* `internal/pipeline` は具体実装に直接依存せず、`ContentReader` インターフェースを通して入力を読み込みます。`ContentReader` は読み取り責務だけを持ち、リソース解放は `internal/app.Container` が管理します。
* `internal/app.Container.Close()` は複数の close エラーを `errors.Join` でまとめて返します。
* `internal/config.Config` は CLI から渡される `SourceURL` の検証に集中しています。

-----

## 🛠️ 主要な依存関係 (Dependencies)

* **[Go Web Exact](https://github.com/shouni/go-web-exact)**: 高精度なメインコンテンツ抽出エンジン。
* **[Go Remote IO](https://github.com/shouni/go-remote-io)**: マルチクラウド I/O 抽象化レイヤー。

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
