# 📖 Go Web Reader

[![CI](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://go.dev/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://go.dev/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)

## 🚀 概要 (About) - Web の本文抽出も GCS / S3 の読み取りも、同じ `Open(ctx, uri)` で

**Go Web Reader** は、Web サイトの本文抽出とクラウドストレージ（GCS / S3）の読み取りを、単一のインターフェースで
扱う Go 言語向け**ライブラリ**です。`https://`、`http://`、`gs://`、`s3://` の URI を渡すだけで、背後の
アクセス手段の違いを意識せずコンテンツを `io.ReadCloser` として取得できます。

> **扱えるのはこの 4 スキームだけです。** ローカルファイルパスは対象外で、`未対応のURIスキームです` を返します。
> ローカルファイルは標準ライブラリで直接読んでください。**大文字のスキーム（`HTTPS://`）も未対応です** —
> スキームの判定は `url.Parse` と違って正規化しません。

### go-remote-io との線引き

姉妹ライブラリの [go-remote-io](https://github.com/shouni/go-remote-io) も `gs://` を読みますが、
担当している工程が違います。両方に表を置くと必ず片方が古くなるので、ここにだけ置いています。

| | go-remote-io | go-web-reader |
| --- | --- | --- |
| 方向 | 読み書き両方（＋署名付き URL・一覧） | **読み取り専用** |
| 対象 | `gs://` / `s3://` / ローカル | `https://` / `gs://` / `s3://`（**ローカルは非対応**） |
| 立ち位置 | **成果物の置き場** | **素材の取得元** |
| HTML | バイト列としてそのまま | **本文だけを抽出**（広告・ナビゲーションを除去） |

-----

## ✨ 提供機能 (Features)

* **スキームを問わない読み取り** — HTTP/HTTPS・GCS・S3 を同じ `Open(ctx, uri)` で扱えます。読み切ってよいなら
  開いて・読んで・閉じるまでを畳んだ `ReadAll` があります。
* **Content-Type による自動切り替え** — HTML は本文抽出、テキストや画像はそのまま返却、それ以外はエラー
  （[対応表](#-対応-content-type)）。
* **高精度な本文抽出** — DOM 構造から広告・ナビゲーションを除いた本文だけを返します
  （走査順・セレクタ・出力の形は [`extract` の godoc](https://pkg.go.dev/github.com/shouni/go-web-reader/extract)）。
  `extract` パッケージは通信を一切せず、渡された `io.Reader` を解析するだけなので単体でも使えます。
* **文字コードの自動判定** — BOM・`Content-Type` の `charset`・`<meta charset>`・本文のバイト列から判定し、
  Shift_JIS / EUC-JP のページも文字化けさせずに読めます。
* **一時的な失敗の再試行** — 5xx / 408 / 429 と分類できない通信エラーを指数バックオフでやり直します
  （回数と待機時間は `WithMaxRetries` / `WithRetryInterval`）。
* **多層の URL 安全性検証** — 取得前の URI 検証に加え、接続直前にも IP を検証します
  （[SSRF 対策](#-url-の安全性-ssrf-対策)）。
* **スキームごとの遅延初期化** — GCS/S3 クライアントは対象スキームの初回 `Open` 時にだけ生成され、以後
  キャッシュされます。ロックもスキームごとに独立しているため、GCS の初期化が詰まっても S3 の呼び出しは
  待たされません。

-----

## 📦 パッケージ構成 (Package Structure)

```text
go-web-reader/
├── reader/     # 【PUBLIC】ユニバーサル・リーダー本体
│               #   スキーム振り分け / Content-Type 分岐 /
│               #   GCS・S3 の遅延初期化 / 依存差し替えオプション
└── extract/    # 【PUBLIC】HTML 本文抽出エンジン（単体でも使えます）
```

依存の向きは `reader` → `extract` の一方向です。`extract` は `reader` の型を一切名指ししません。

-----

## 🚦 使い方 (Usage)

```go
import "github.com/shouni/go-web-reader/reader"

r := reader.New()
defer func() { _ = r.Close() }()

body, err := r.ReadAll(ctx, "https://example.com/article") // gs:// / s3:// も同じ呼び方
```

大きなオブジェクトを流したいときは `Open(ctx, uri)` が `io.ReadCloser` を返します。`io.ReadAll` で
文字列にせず `io.Copy` で流せば、消費メモリはコピーバッファ分で済みます。

`reader.New()` はエラーを返しません。ここで確立する外部接続がないためです。GCS/S3 クライアントは対象スキームの
初回 `Open` まで作られず、失敗するとしたらそちらです。**`Close` は終端です** — 解放後の `Open` は
クライアントを作り直さず `ErrClosed` を返します（`https://` も含め、スキームを問いません）。

取得済みの HTML が手元にあるなら `extract.Text(ctx, r)` を直接呼べます。`Content-Type` ヘッダーが手元に
あるなら、それを判定材料に加える `extract.TextWithContentType(ctx, r, contentType)` を使ってください —
**HTML の文字コードはヘッダーの `charset` が最優先で、`charset` を名乗らない Shift_JIS のページはこれが
唯一の手がかりになります。** `reader` 経由ならこの受け渡しは自動です（`WithExtractor` に渡した抽出器が
`reader.ContentTypeExtractor` も満たしていれば、そちらが呼ばれます）。

### 依存の差し替え (`Option`)

テストや組み込みで実ネットワーク・実クラウドを避けるための差し替え口です。既定は SSRF 対策付きの
`httpkit.New`、`extract.Engine{}`、`securenet.ValidateURL`、`gcs.New` / `s3.New` で、
**`nil` を渡したオプションは無視され、既定値が保たれます**（差し替えたつもりで既定のままになります）。
一覧と既定値は [pkg.go.dev](https://pkg.go.dev/github.com/shouni/go-web-reader/reader) にあります。

`WithExtractor` が差し替えるのは抽出エンジンだけで、HTTP の取得は差し替わりません。取得側を変えたいときは
`WithHTTPClient` です。

**踏むと高くつく点も、それぞれの godoc に書いてあります** — 再試行する失敗の種類と `Retry-After` の扱い
（`WithMaxRetries`）、差し替えても外れないレスポンスサイズ上限（`WithHTTPClient`）、検証器を緩めても
接続直前の IP 検証は残ること（`WithSafeURLValidator`）、抽出のしきい値を文字数で測ること（`extract`）。

-----

## 📋 対応 Content-Type

HTTP(S) では、レスポンスの media type で挙動が決まります。

* `text/html`, `application/xhtml+xml` — 抽出エンジンにかけ、**本文テキストのみ**を返す
* `text/plain`, `text/markdown`, `text/x-markdown` — 変換せずそのまま返す
* `image/*`（サブタイプ不問） — 変換せず生バイト列のまま返す
* 上記以外 — 未対応エラー

`Content-Type` ヘッダーが RFC に沿わない場合（`charset="` の閉じ忘れなど、実在するサーバーが返してくるもの）
でも、`;` より前が既知の media type と一致すればそれを採用します。未知の media type まで救うと壊れたヘッダーを
根拠に中身を誤解釈することになるため、その場合は解析エラーを返します。

-----

## 🛡️ URL の安全性 (SSRF 対策)

Web URL に対しては 2 段階の防御が働きます。

1. **取得前の検証** — `securenet.ValidateURL` が名前解決まで行い、プライベート / ループバック /
   リンクローカルなどの制限ネットワークを拒否します。
2. **接続直前の検証** — 既定の HTTP クライアントが接続の直前にも IP を検証するため、DNS Rebinding も
   防げます。あわせて環境変数プロキシの無効化、リダイレクト追従の上限、`https` → `http` ダウングレードの
   拒否も適用されます。

検証が掛かる範囲（HTTP(S) の枝だけ）と、ローカルのテストサーバーへ向けるときの差し替え方は
[`WithSafeURLValidator` の godoc](https://pkg.go.dev/github.com/shouni/go-web-reader/reader#WithSafeURLValidator) にあります。

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
