# 📖 Go Web Reader

[![CI](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)
[![Status](https://img.shields.io/badge/Status-Completed-brightgreen)](#)

## 🚀 概要 (About) — Web とクラウドストレージを扱うユニバーサル・リーダー

**Go Web Reader** は、Web サイトの本文抽出とクラウドストレージ（GCS/S3）の読み取りを、単一のインターフェースで扱う Go 言語向け**ライブラリ**です。

`https://`、`gs://`、`s3://` の **URI** を渡すだけで、背後のアクセス手段の違いを意識せずコンテンツを `io.ReadCloser` として取得できます。

> **扱えるのは上記 3 スキームだけです。** ローカルファイルパスは対象外で、`未対応のURIスキームです` を返します。ローカルファイルは標準ライブラリで直接読んでください。

-----

## ✨ 提供機能 (Features)

* **スキームを問わない読み取り** — HTTP/HTTPS・GCS・S3 を同じ `Open(ctx, uri)` で扱えます。
* **Content-Type による自動切り替え** — HTML は本文抽出、テキストや画像はそのまま返却、それ以外はエラー（[対応表](#-対応-content-type)）。
* **高精度な本文抽出** — DOM 構造から広告・ナビゲーションを除いた本文だけを返します（[抽出ルール](#-本文抽出のルール)）。
* **多層の URL 安全性検証** — 取得前の URI 検証に加え、接続直前にも IP を検証します（[SSRF 対策](#-url-の安全性-ssrf-対策)）。
* **スキームごとの遅延初期化** — GCS/S3 クライアントは対象スキームの初回 `Open` 時にだけ生成され、以後キャッシュされます。ロックもスキームごとに独立しているため、GCS の初期化が詰まっても S3 の呼び出しは待たされません。

-----

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-web-reader/
├── reader/     # 【PUBLIC】ユニバーサル・リーダー本体
│               #   スキーム振り分け / Content-Type 分岐 /
│               #   GCS・S3 の遅延初期化 / 依存差し替えオプション
└── extract/    # 【PUBLIC】HTML 本文抽出エンジン（単体でも使えます）
```

依存の向きは `reader` → `extract` の一方向です。`extract` は通信を一切せず、渡された `io.Reader` を解析するだけなので、HTTP でもファイルでもテスト用の文字列でも同じ経路で扱えます。

-----

## 🚦 使い方 (Usage)

### URI からコンテンツを読む

```go
import (
    "context"
    "io"
    "os"

    "github.com/shouni/go-web-reader/reader"
)

func read(ctx context.Context, uri string) error {
    r := reader.New()
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

`reader.New()` はエラーを返しません。ここで確立する外部接続がないためです。GCS/S3 クライアントは対象スキームの初回 `Open` まで作られず、失敗するとしたらそちらです。

**`Close` は終端です。** 解放後の `Open` はクライアントを作り直さず `ErrClosed` を返します（`errors.Is(err, reader.ErrClosed)` で判定できます）。複数の close エラーは握りつぶさず `errors.Join` でまとめて返します。

### HTML から本文だけ抜き出す

取得済みの HTML が手元にあるなら、`extract` を直接使えます。

```go
import "github.com/shouni/go-web-reader/extract"

text, hasBody, err := extract.Text(ctx, resp.Body)
```

`hasBody` は本文が見つかったかどうかです。タイトルしか取れなかった場合はテキストを返しつつ `false` になります（エラーではありません）。

### 依存の差し替え (`Option`)

テストや組み込みで実ネットワーク・実クラウドを避けるための差し替え口です。`nil` を渡したオプションは無視され、既定値が保たれます。

| オプション | 差し替える対象 | 既定値 |
| :--- | :--- | :--- |
| `WithHTTPClient` | HTTP リクエストを実行するクライアント | `httpkit.New`（SSRF 対策付き） |
| `WithExtractor` | HTML からの本文抽出エンジン | `extract.Engine{}` |
| `WithSafeURLValidator` | 取得前の URI 安全性検証 | `securenet.ValidateURL` |
| `WithGCSFactory` | GCS クライアントの生成 | `gcs.New` |
| `WithS3Factory` | S3 クライアントの生成 | `s3.New` |

`WithExtractor` が差し替えるのは抽出エンジンだけで、HTTP の取得は差し替わりません。取得側を変えたいときは `WithHTTPClient` を使います。リクエストヘッダー（User-Agent など）も、`Do` の中で受け取った `*http.Request` を書き換えてから委譲すれば変更できます。

```go
type headerOverride struct{ inner reader.HTTPClient }

func (h headerOverride) Do(req *http.Request) (*http.Response, error) {
    req.Header.Set("User-Agent", "my-agent/1.0")
    return h.inner.Do(req)
}
```

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

なお取得はどの Content-Type でも同じ経路を通り、レスポンスサイズ上限（**25MB**）が必ず適用されます。`WithHTTPClient` で差し替えても、上限をかけるのはクライアントの外側なので外れません。

-----

## 📑 本文抽出のルール

`extract` は「記事本文だけを残す」ことを目的にしたヒューリスティックです。

**1. ノイズを落とす** — `script`、`style`、`form`、`nav`、`aside`、および広告・SNS・コメント欄まわりのクラス（`.ad-banner`、`.social-share`、`.comments` など）をページ全体から除去します。

**2. 本文の範囲を決める** — `article` / `main` / `div[role=main]` / `#content` / `.entry-content` などに最初に一致した要素を本文とします。見つからない場合はページ全体を本文とみなし、そのときだけ `header` / `footer` / `.sidebar` も落とします（記事の内側の `header` は見出しを、`footer` は署名を含むことがあるため、常に落とすと本文が欠けます）。

**3. ブロック要素を順に拾う** — `p`、`h1`〜`h6`、`li`、`blockquote`、`table`、`pre` を DOM の出現順に走査します。入れ子（`<li><p>…</p></li>` など）は一度だけ出力されます。

| 要素 | 出力 |
| :--- | :--- |
| `title` | `【記事タイトル】 ` を付けて先頭に |
| `h1`〜`h6` | `## ` を付ける（**2 文字**以上のもの） |
| `p`, `blockquote` | **20 文字**以上のものだけ |
| `li` | 長さを問わず出力（項目は短くても意味を持つため） |
| `table` | `【表題】 ` 付きキャプション＋`セル \| セル` の行（空行は出力しない） |
| `pre` | ` ``` ` のコードフェンスで囲む |

しきい値はバイト数ではなく**文字数**で測ります。定数として `extract.MinParagraphLength` / `extract.MinHeadingLength` を公開しています。

-----

## 🛡️ URL の安全性 (SSRF 対策)

Web URL に対しては 2 段階の防御が働きます。

1. **取得前の検証** — `securenet.ValidateURL` が名前解決まで行い、プライベート / ループバック / リンクローカルなどの制限ネットワークを拒否します。
2. **接続直前の検証** — 既定の HTTP クライアント（`securenet.NewSafeHTTPClient`）が接続の直前にも IP を検証するため、DNS Rebinding も防げます。あわせて環境変数プロキシの無効化、リダイレクト追従の上限（10 回）、`https` → `http` ダウングレードの拒否も適用されます。

`gs://` / `s3://` はクラウド SDK が接続先を決めるため、名前解決を伴う検証は行われません。

> **ローカルのテストサーバーへ向けたい場合**は既定のままでは遮断されます。`WithSafeURLValidator` と `WithHTTPClient` の両方を差し替えてください（片方だけでは接続直前の検証に引っかかります）。

-----

## 🛠️ 主要な依存関係 (Dependencies)

* **[Go Remote IO](https://github.com/shouni/go-remote-io)**: マルチクラウド I/O 抽象化レイヤー。スキームの解釈もこちらに合わせています。
* **[goquery](https://github.com/PuerkitoBio/goquery)** + `golang.org/x/net/html`: `extract` の DOM 走査。
* **[Go HTTP Kit](https://github.com/shouni/go-http-kit)**: 既定の HTTP クライアントとレスポンス処理。
* **[netarmor](https://github.com/shouni/netarmor)**: URL・接続先 IP の安全性検証（SSRF 対策）。

-----

## 📌 v1.3.0 での変更 (Breaking Changes)

CLI を廃止してライブラリ専用にし、[`go-web-exact`](https://github.com/shouni/go-web-exact) の抽出エンジンを取り込みました。

### API

| 変更前 | 変更後 |
| :--- | :--- |
| `go-web-reader/pkg/reader` | `go-web-reader/reader` |
| `go-web-exact/v2/extract` | `go-web-reader/extract` |
| `r, err := reader.New(...)` | `r := reader.New(...)`（エラーを返さない） |
| `ports.Extractor` | `reader.Extractor` |
| `Extractor.ExtractText(...)` | `Extractor.Extract(...)`（メソッド名がインターフェース名を反復していたため） |
| `ports.Fetcher` / `WithFetcher` | 廃止。`WithHTTPClient` で代替できます（上記の例を参照） |
| `extract.NewExtractor(fetcher)` | `extract.Text(ctx, r)`、DI 用は `extract.Engine{}` |
| `go-web-reader read --uri ...`（CLI） | 廃止 |

### 抽出結果の変化

取り込みにあわせて `extract` の不具合を修正したため、同じページでも出力が変わります。

* `article` / `main` の無いページで、**ナビゲーションやフッターのリンクが混入しなくなりました**（囲み要素の除外が実際には効いていませんでした）。
* `<li><p>…</p></li>` のような入れ子が**二重に出力されなくなりました**。
* しきい値をバイト数から文字数に変更したため、**日本語の短い段落が本文から外れるようになりました**（従来は 20 文字のつもりが実質 7 文字程度で通っていました）。`MinHeadingLength` は 3 から 2 に変更し、「概要」のような 2 文字見出しを残します。
* 空の `<tr>` が空行として出力されなくなりました。

`go-web-exact` の `scraper` / `runner` / `builder`（並列取得・リトライ）は利用者がいなかったため引き継いでいません。必要になったら同リポジトリの `v2.5.2` タグから取り出してください。

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
