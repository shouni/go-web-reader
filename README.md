# 📖 Go Web Reader

[![CI](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://go.dev/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://go.dev/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)

## 🚀 概要 (About) - Web の本文抽出も GCS / S3 の読み取りも、同じ `Open(ctx, uri)` で

**Go Web Reader** は、Web ページの本文抽出とクラウドストレージ（GCS / S3）の読み取りを、単一のインターフェースで
扱う Go 言語向け**ライブラリ**です。`https://`、`http://`、`gs://`、`s3://` の URI を渡すだけで、背後の
アクセス手段の違いを意識せずコンテンツを `io.ReadCloser` として取得できます。

引き受けないもの: **書き込み**（[go-remote-io](https://github.com/shouni/go-remote-io) の担当）と
**ローカルファイル**（標準ライブラリで直接読んでください）。

### go-remote-io との線引き

姉妹ライブラリの [go-remote-io](https://github.com/shouni/go-remote-io) も `gs://` を読みますが、
担当している工程が違います。この表はここにだけ置いています。

| | go-remote-io | go-web-reader |
| --- | --- | --- |
| 方向 | 読み書き両方（＋署名付き URL・一覧） | **読み取り専用** |
| 対象 | `gs://` / `s3://` / ローカル | `https://` / `gs://` / `s3://`（**ローカルは非対応**） |
| 立ち位置 | **成果物の置き場** | **素材の取得元** |
| HTML | バイト列としてそのまま | **本文だけを抽出**（HTTP(S) のみ。`gs://` / `s3://` の HTML はそのまま） |

-----

## ✨ 提供機能 (Features)

* **スキームを問わない読み取り** — HTTP/HTTPS・GCS・S3 を同じ `Open(ctx, uri)` で扱えます。読み切ってよいなら
  `ReadAll` があります。GCS/S3 のクライアントは対象スキームの初回 `Open` まで作られません。
* **Content-Type による自動切り替え** — HTML は本文抽出、テキストや画像はそのまま返却、それ以外はエラー
  （[対応表](#-対応-content-type)）。
* **本文抽出** — DOM 構造から広告・ナビゲーションを除いた本文だけを返します。Shift_JIS / EUC-JP のページも
  文字コードを判定して読めます（走査順・セレクタ・出力の形は
  [`extract` の godoc](https://pkg.go.dev/github.com/shouni/go-web-reader/extract)）。
  `extract` は通信を一切せず、渡された `io.Reader` を解析するだけなので単体でも使えます。
* **一時的な失敗の再試行** — 5xx や通信エラーを指数バックオフでやり直します
  （`WithMaxRetries` / `WithRetryInterval`）。
* **SSRF 対策** — 取得前の URL 検証に加え、接続直前にも IP を検証します
  （[URL の安全性](#-url-の安全性-ssrf-対策)）。

-----

## 📦 パッケージ構成 (Package Structure)

```text
go-web-reader/
├── reader/     # 【PUBLIC】ユニバーサル・リーダー本体
│               #   スキーム振り分け / Content-Type 分岐 /
│               #   GCS・S3 の遅延初期化 / 依存差し替えオプション
└── extract/    # 【PUBLIC】HTML 本文抽出エンジン（単体でも使えます）
```

依存の向きは `reader` → `extract` の一方向です。

-----

## 🚦 使い方 (Usage)

```go
import "github.com/shouni/go-web-reader/reader"

r := reader.New()
defer func() { _ = r.Close() }()

body, err := r.ReadAll(ctx, "https://example.com/article") // gs:// / s3:// も同じ呼び方
```

**ストリームになるのは `gs://` / `s3://` だけです。** HTTP(S) は本文抽出と再試行のためにレスポンスを読み切って
から返すので、`Open` の `io.ReadCloser` を `io.Copy` で流してもメモリは節約になりません
（上限は `go-http-kit` のレスポンスサイズ上限）。

取得済みの HTML が手元にあるなら `extract.Text` を直接呼べます。`Content-Type` ヘッダーもあるなら
`extract.TextWithContentType` に渡してください — `<meta charset>` を名乗らない Shift_JIS のページは
ヘッダーの `charset` が唯一の手がかりです。

### 依存の差し替え (`Option`)

テストや組み込みで実ネットワーク・実クラウドを避けるための差し替え口です。既定は SSRF 対策付きの
`httpkit.New`、`extract.Engine{}`、`securenet.ValidateURL`、`gcs.New` / `s3.New` です。
一覧は [pkg.go.dev](https://pkg.go.dev/github.com/shouni/go-web-reader/reader) にあり、差し替えたときの
罠（再試行の重複、外れないサイズ上限、検証器を緩めても残る IP 検証）は各 `With*` の godoc に書いてあります。

-----

## 📋 対応 Content-Type

HTTP(S) では、レスポンスの media type で挙動が決まります。

* `text/html`, `application/xhtml+xml` — 抽出エンジンにかけ、**本文テキストのみ**を返す
* `text/plain`, `text/markdown`, `text/x-markdown`, `text/csv`, `application/json`, `application/xml`,
  `text/xml` — 変換せずそのまま返す
* `image/*`（サブタイプ不問） — 変換せず生バイト列のまま返す
* 上記以外 — 未対応エラー

RFC に沿わない `Content-Type`（`charset="` の閉じ忘れなど）でも、`;` より前が上記の media type なら採用します。

-----

## 🛡️ URL の安全性 (SSRF 対策)

HTTP(S) では 2 段階の防御が働きます。

1. **取得前** — `securenet.ValidateURL` が名前解決まで行い、プライベート / ループバック / リンクローカル宛てを拒否します。
2. **接続直前** — 既定の HTTP クライアントが接続先 IP を再検証するため、DNS Rebinding も防げます
   （クライアント自身の保護は [go-http-kit](https://github.com/shouni/go-http-kit) の担当です）。

`gs://` / `s3://` は接続先をクラウド SDK が決めるため、この検証は掛かりません。

-----

## 🤝 依存関係 (Dependencies)

* [`github.com/PuerkitoBio/goquery`](https://github.com/PuerkitoBio/goquery) — `extract` の DOM 走査
* [`github.com/andybalholm/cascadia`](https://github.com/andybalholm/cascadia) — CSS セレクタの事前コンパイル
  （goquery の内部エンジン）
* [`github.com/shouni/go-http-kit`](https://github.com/shouni/go-http-kit) — 既定の HTTP クライアントと
  レスポンス処理（サイズ上限）、取得のリトライ
* [`github.com/shouni/go-remote-io`](https://github.com/shouni/go-remote-io) — GCS/S3 の I/O 抽象化。
  スキームの解釈もこちらに合わせています
* [`github.com/shouni/netarmor`](https://github.com/shouni/netarmor) — URL・接続先 IP の安全性検証（SSRF 対策）
* [`golang.org/x/net`](https://pkg.go.dev/golang.org/x/net) — `html`（テキストノード走査）と
  `html/charset`（文字コード判定）

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
