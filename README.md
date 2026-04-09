# 📖 Go Web Reader

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 🚀 概要 (About) — Web とクラウドストレージを扱うユニバーサル・リーダー

**Go Web Reader** は、Web サイトの本文抽出とクラウドストレージ（GCS/S3）の読み取りを、単一のインターフェースで扱う Go 言語向けライブラリです。

`https://`、`gs://`、`s3://` といった **URI** を渡すだけで、背後のアクセス手段の違いを意識せずにコンテンツを取得できます。
公開 API の中心は `pkg/reader` で、`reader.New()` でリーダーを生成し、`Read(ctx, uri)` 呼び出し時に URI スキームに応じて処理を切り替えます。

-----

## ✨ 提供機能 (Features)

### 🌐 [extract] ユニバーサル・コンテンツ抽出

* **Unified Interface**: URL が指定された場合、自動的に Web 抽出エンジンが起動。ノイズ（広告・ナビゲーション）を除去した「本文のみ」を即座にストリームとして返します。
* **Heuristic Engine**: DOM 構造を解析し、文脈を維持したまま高精度にメインテキストを特定します。

### ☁️ [remote] マルチプロトコル I/O

* **Storage Agnostic**: GCS と S3 を同じ `Read(ctx, uri)` インターフェースで扱えます。
* **Lazy Initialization**: GCS/S3 のクライアントは必要になった時だけ初期化されます。

### ⚡ [orchestration] 実行オーケストレーション

* **CLI Entry Point**: `cmd/read.go` から URI を受け取り、パイプライン経由で結果を標準出力に出力します。
* **Dependency Injection**: `internal/builder` で `reader` と `pipeline` を組み立て、`internal/pipeline` は抽象化された `Reader` に依存します。

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
│       └── reader_test.go # - スキーム分岐とリソース管理のテスト
└── internal/
    ├── app/            # アプリケーション層
    │   └── container.go #   - 実行時コンテナとクローズ対象の管理
    ├── builder/        # 依存関係の注入 (DI)
    │   ├── app.go      #   - アプリケーション全体の構築
    │   └── pipeline.go #   - 処理パイプラインの構成
    ├── config/         # 設定管理
    │   └── config.go   #   - フラグ構造体の定義と検証
    ├── domain/         # ドメイン層
    │   └── ports.go    #   - 共通インターフェース・抽象定義
    └── pipeline/       # ビジネスロジック / 実行フロー
        └── pipeline.go #   - Reader を使ったコンテンツ取得シーケンス
```

-----

## 🔧 実装メモ (Implementation Notes)

* `pkg/reader.New()` は軽量な初期化だけを行い、実際の GCS/S3 クライアント生成は `Read(ctx, uri)` の呼び出し時に遅延実行されます。
* `internal/pipeline` は具体実装に直接依存せず、`Reader` インターフェースを通して入力を読み込みます。
* `internal/app.Container` は、アプリケーション終了時に `Close()` すべき依存をまとめて管理します。

-----

## 🛠️ 主要な依存関係 (Dependencies)

* **[Go Web Exact](https://github.com/shouni/go-web-exact)**: 高精度なメインコンテンツ抽出エンジン。
* **[Go Remote IO](https://github.com/shouni/go-remote-io)**: マルチクラウド I/O 抽象化レイヤー。

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
