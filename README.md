# 📖 Go Web Reader

[![CI](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)
[![Status](https://img.shields.io/badge/Status-Completed-brightgreen)](#)

## 🚀 概要 (About) — Web とクラウドストレージを扱うユニバーサル・リーダー

**Go Web Reader** は、Web サイトの本文抽出とクラウドストレージ（GCS/S3）の読み取りを、単一のインターフェースで扱う Go 言語向けライブラリ + CLI です。

`https://`、`gs://`、`s3://` の **URI** を渡すだけで、背後のアクセス手段の違いを意識せずコンテンツを `io.ReadCloser` として取得できます。公開 API の中心は `pkg/reader` で、`reader.New()` で生成し `Open(ctx, uri)` を呼ぶだけです。

> **扱えるのは上記 3 スキームだけです。** ローカルファイルパスは対象外で、`未対応のURIスキームです` を返します。ローカルファイルは標準ライブラリで直接読んでください。

-----

## ✨ 提供機能 (Features)

### 🌐 コンテンツ抽出 (HTTP/HTTPS)

* **Content-Type による自動切り替え**: レスポンスの media type を見て、本文抽出・そのまま返却・エラーを切り替えます（[対応表](#-対応-content-type)）。
* **高精度な本文抽出**: HTML は DOM 構造を解析するヒューリスティックエンジンにかけ、広告やナビゲーションを除いた本文だけをストリームとして返します。
* **多層の URL 安全性検証**: 取得前の URI 検証に加え、既定の HTTP クライアントが接続直前にも IP を検証します（[安全性](#-url-の安全性-ssrf-対策)）。

### ☁️ マルチプロトコル I/O (GCS/S3)

* **Storage Agnostic**: GCS と S3 を HTTP と同じ `Open(ctx, uri)` で扱えます。
* **スキームごとの遅延初期化**: クライアントは対象スキームの初回 `Open` 時にだけ生成され、以後キャッシュされます。ロックもスキームごとに独立しているため、GCS の初期化が詰まっても S3 の呼び出しは待たされません。

### ⚡ 実行オーケストレーション (CLI)

* **パイプに乗る出力**: 取得内容を標準出力へ**ストリーム**します。見出しや区切り線は標準エラーへ出すため、リダイレクトやパイプにそのまま渡せます。内容を一度もメモリに溜めないため、大きなオブジェクトでも消費メモリは一定です。
* **リソース安全性**: close エラーは握りつぶさず `errors.Join` でまとめて返します。

-----

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-web-reader/
├── cmd/                  # Cobra CLI（フラグ定義、read サブコマンド、出力の振り分け）
├── pkg/
│   └── reader/           # 【PUBLIC】ユニバーサル・リーダー本体
│                         #   スキーム振り分け / HTTP と Content-Type 分岐 /
│                         #   GCS・S3 の遅延初期化 / 依存差し替えオプション
└── internal/
    ├── builder/          # 依存関係の注入 (DI)：Config から Container を組み立てる
    ├── app/              # 実行時コンテナとクローズ対象の管理
    ├── pipeline/         # ContentReader から読み、io.Writer へ書き出す実行フロー
    ├── config/           # フラグ構造体の定義と検証
    └── closeutil/        # 複数クローザーの実行とエラー結合
```

依存の向きは `cmd` → `builder` → `app` / `pipeline` です。`internal/pipeline` は `pkg/reader` を直接 import せず、自前の `ContentReader` インターフェースに依存します。これがテストを実ネットワーク・実クラウドなしで回せる理由です。

-----

## 🚦 使い方 (Usage)

### CLI

```bash
go run . read --uri https://example.com/article
go run . read --uri gs://bucket/path/to/file.txt
go run . read --uri s3://bucket/path/to/file.txt

# 標準出力は取得内容のみなので、そのままリダイレクト・パイプできる
go run . read --uri https://example.com/article > article.txt
```

### Library

```go
import (
    "context"
    "io"
    "os"

    "github.com/shouni/go-web-reader/pkg/reader"
)

func read(ctx context.Context, uri string) error {
    r, err := reader.New()
    if err != nil {
        return err
    }
    defer func() { _ = r.Close() }()

    stream, err := r.Open(ctx, uri)
    if err != nil {
        return err
    }
    defer func() { _ = stream.Close() }()

    // io.ReadAll で文字列にせずそのまま流せば、大きなオブジェクトでも
    // 消費メモリはコピーバッファ分で済みます。
    _, err = io.Copy(os.Stdout, stream)

    return err
}
```

### 依存の差し替え (`Option`)

テストや組み込みで実ネットワーク・実クラウドを避けるための差し替え口です。`nil` を渡したオプションは無視され、既定値が保たれます。

| オプション | 差し替える対象 | 既定値 |
| :--- | :--- | :--- |
| `WithHTTPClient` | HTTP リクエストを実行するクライアント | `httpkit.New`（SSRF 対策付き） |
| `WithFetcher` | HTTP 取得処理そのもの（リクエスト組み立て〜レスポンス処理） | 上記クライアントを使う既定実装 |
| `WithExtractor` | HTML からの本文抽出エンジン | `go-web-exact` |
| `WithSafeURLValidator` | 取得前の URI 安全性検証 | `securenet.ValidateURL` |
| `WithGCSFactory` | GCS クライアントの生成 | `gcs.New` |
| `WithS3Factory` | S3 クライアントの生成 | `s3.New` |

`WithExtractor` が差し替えるのは抽出エンジンだけです。HTTP の取得そのものを差し替えるなら `WithFetcher`（または `WithHTTPClient`）を使ってください。

-----

## 📋 対応 Content-Type

HTTP(S) では、レスポンスの media type で挙動が決まります。

| media type | 挙動 |
| :--- | :--- |
| `text/html`, `application/xhtml+xml` | 抽出エンジンにかけ、**本文テキストのみ**を返す |
| `text/plain`, `text/markdown`, `text/x-markdown` | 変換せずそのまま返す |
| `image/*`（サブタイプ不問） | 変換せず生バイト列のまま返す |
| 上記以外 | 未対応エラー |

`Content-Type` ヘッダーが RFC に沿わない場合（`charset="` の閉じ忘れなど、実在するサーバーが返してくるもの）でも、`;` より前が既知の media type と一致すればそれを採用します。未知の media type まで救うと壊れたヘッダーを根拠に中身を誤解釈することになるため、その場合は解析エラーを返します。

-----

## 🛡️ URL の安全性 (SSRF 対策)

Web URL に対しては 2 段階の防御が働きます。

1. **取得前の検証** — `securenet.ValidateURL` が名前解決まで行い、プライベート / ループバック / リンクローカルなどの制限ネットワークを拒否します。
2. **接続直前の検証** — 既定の HTTP クライアント（`securenet.NewSafeHTTPClient`）が接続の直前にも IP を検証するため、DNS Rebinding も防げます。あわせて環境変数プロキシの無効化、リダイレクト追従の上限（10 回）、`https` → `http` ダウングレードの拒否も適用されます。

`gs://` / `s3://` はクラウド SDK が接続先を決めるため、名前解決を伴う検証は行われません。

> **ローカルのテストサーバーへ向けたい場合**は既定のままでは遮断されます。`WithSafeURLValidator` と `WithHTTPClient` の両方を差し替えてください（片方だけでは接続直前の検証に引っかかります）。

-----

## 🔧 実装メモ (Implementation Notes)

* **`Close` は終端です。** GCS/S3 の reader と closer は初回アクセス後キャッシュされ、`Close()` でまとめて解放されます。解放後の `Open` は `ErrClosed` を返し、クライアントを作り直しません（`errors.Is(err, reader.ErrClosed)` で判定できます）。
* HTTP(S) レスポンスボディの取得は `go-http-kit` の `FetchBytes`（`HandleResponse`）に一本化されており、Content-Type を問わず **25MB** の読み込み上限が適用されます。超過した場合はエラーになります。
* スキームの振り分けは `remoteio.SchemePrefix` を使い、`go-remote-io` と同じ解釈を共有します。判定を自前で書くと「どこからがスキームか」が両者でずれるためです。
* `internal/pipeline` は `Execute(ctx, io.Writer)` で読み取りストリームを書き出し先へ直接流します。途中で文字列にしないため、消費メモリは対象サイズに依存しません。
* `internal/app.Container.Close()` は複数の close エラーを `errors.Join` でまとめて返します。
* `internal/config.Config` は CLI から渡される `SourceURL` の検証に集中しています。

-----

## 🛠️ 主要な依存関係 (Dependencies)

* **[Go Web Exact](https://github.com/shouni/go-web-exact)**: 高精度なメインコンテンツ抽出エンジン。
* **[Go Remote IO](https://github.com/shouni/go-remote-io)**: マルチクラウド I/O 抽象化レイヤー。
* **[Go HTTP Kit](https://github.com/shouni/go-http-kit)**: 既定の HTTP クライアントとレスポンス処理。
* **[netarmor](https://github.com/shouni/netarmor)**: URL・接続先 IP の安全性検証（SSRF 対策）。

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
