# 📖 Go Web Reader

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 🚀 概要 (About) — Web とクラウドストレージを統合するユニバーサル・リーダー

**Go Web Reader** は、Web サイトのメインコンテンツ抽出と、マルチクラウド（GCS/S3/Local）のファイル I/O を単一のインターフェースで統合する Go 言語向けライブラリです。

`https://`、`gs://`、`s3://`、あるいはローカルパスといった **URI** を渡すだけで、背後のストレージの違いや Web 解析の複雑さを意識することなく、クリーンなデータを `io.ReadCloser` として取得できます。

-----

## ✨ 提供機能 (Features)

### 🌐 [extract] ユニバーサル・コンテンツ抽出

* **Unified Interface**: URL が指定された場合、自動的に Web 抽出エンジンが起動。ノイズ（広告・ナビゲーション）を除去した「本文のみ」を即座にストリームとして返します。
* **Heuristic Engine**: DOM 構造を解析し、文脈を維持したまま高精度にメインテキストを特定します。

### ☁️ [remote] マルチプロトコル I/O

* **Storage Agnostic**: GCS、S3、ローカルファイルシステムを透過的に扱えます。
* **Seamless Integration**: クラウド上のドキュメント読み込みと Web サイトのスクレイピングを、全く同じコードパスで記述可能です。

### ⚡ [orchestration] 実行オーケストレーション

* **Concurrent Scraper**: `errgroup` による並列処理とレート制限を内蔵し、大量のリソースを一括で安全に読み込みます。
* **Robust Runner**: 一時的なネットワークエラーやコンテンツ未検出時の自動リトライ戦略を標準搭載。

-----

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-web-reader/
├── cmd/                # CLI コマンド定義
│   ├── read.go         #   - 'read' サブコマンドの実装
│   └── root.go         #   - ルートコマンド・フラグ・初期化
└── internal/
    ├── app/            # アプリケーション層
    │   └── container.go #   - 実行時コンテナ（ライフサイクル・リソース管理）
    ├── builder/        # 依存関係の注入 (DI)
    │   ├── app.go      #   - アプリケーション全体の構築
    │   ├── io.go       #   - 入出力関連の初期化
    │   └── pipeline.go #   - 処理パイプラインの構成
    ├── config/         # 設定管理
    │   └── config.go   #   - 環境変数・フラグ構造体の定義
    ├── domain/         # ドメイン層
    │   └── ports.go    #   - 共通インターフェース・抽象定義
    └── pipeline/       # ビジネスロジック / 実行フロー
        └── pipeline.go #   - コンテンツ取得・変換のメインシーケンス
```

-----

## 🛠️ 主要な依存関係 (Dependencies)

* **[Go Web Exact](https://github.com/shouni/go-web-exact)**: 高精度なメインコンテンツ抽出エンジン。
* **[Go Remote IO](https://github.com/shouni/go-remote-io)**: マルチクラウド I/O 抽象化レイヤー。

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
