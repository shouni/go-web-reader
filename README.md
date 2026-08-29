# 📖 Go Web Reader

[![CI](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://go.dev/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://go.dev/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)

## 🚀 概要 (About) - Web の本文抽出も GCS / S3 の読み取りも、同じインターフェースで

**Go Web Reader** は、Web サイトの本文抽出とクラウドストレージ（GCS/S3）の読み取りを、単一のインターフェースで扱う Go 言語向け**ライブラリ**です。

`https://`、`gs://`、`s3://` の **URI** を渡すだけで、背後のアクセス手段の違いを意識せずコンテンツを `io.ReadCloser` として取得できます。

> **扱えるのは上記 3 スキームだけです。** ローカルファイルパスは対象外で、`未対応のURIスキームです` を返します。ローカルファイルは標準ライブラリで直接読んでください。

-----

## ✨ 提供機能 (Features)

* **スキームを問わない読み取り** — HTTP/HTTPS・GCS・S3 を同じ `Open(ctx, uri)` で扱えます。
* **Content-Type による自動切り替え** — HTML は本文抽出、テキストや画像はそのまま返却、それ以外はエラー（[対応表](#-対応-content-type)）。
* **高精度な本文抽出** — DOM 構造から広告・ナビゲーションを除いた本文だけを返します（[抽出ルール](#-本文抽出のルール)）。
* **文字コードの自動判定** — BOM・`<meta charset>`・`Content-Type` ヘッダーから判定し、Shift_JIS / EUC-JP のページも文字化けさせずに読めます。
* **一時的な失敗の再試行** — 5xx / 408 / 429 と通信エラーは指数バックオフでやり直します（[リトライ](#-リトライ)）。
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

読み切ってから扱うなら `ReadAll` が使えます。開いて・読み切って・閉じるまでを行い、読み取りとクローズのエラーをまとめて返します。

```go
body, err := r.ReadAll(ctx, uri)
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

入力は UTF-8 でなくても構いません。BOM・`<meta charset>`・本文のバイト列から文字コードを判定して UTF-8 に直してから解析します。`Content-Type` ヘッダーが手元にあるなら、そちらを優先材料にする `TextWithContentType` を使ってください（HTML の文字コードはヘッダーの `charset` が最優先で、`charset` を名乗らない Shift_JIS のページはこれが唯一の手がかりになります）。

```go
text, hasBody, err := extract.TextWithContentType(ctx, resp.Body, resp.Header.Get("Content-Type"))
```

`reader` 経由の場合はこの受け渡しが自動で行われます。`WithExtractor` に渡した抽出器が `reader.ContentTypeExtractor`（= `ExtractWithContentType`）も満たしていれば、`Extract` の代わりにそちらが呼ばれます。

### 依存の差し替え (`Option`)

テストや組み込みで実ネットワーク・実クラウドを避けるための差し替え口です。`nil` を渡したオプションは無視され、既定値が保たれます。

| オプション | 差し替える対象 | 既定値 |
| :--- | :--- | :--- |
| `WithHTTPClient` | HTTP リクエストを実行するクライアント | `httpkit.New`（SSRF 対策付き） |
| `WithExtractor` | HTML からの本文抽出エンジン | `extract.Engine{}` |
| `WithSafeURLValidator` | 取得前の URI 安全性検証（HTTP(S) のみ） | `securenet.ValidateURL` |
| `WithGCSFactory` | GCS クライアントの生成 | `gcs.New` |
| `WithS3Factory` | S3 クライアントの生成 | `s3.New` |
| `WithMaxRetries` | HTTP 取得を再試行する回数 | `2`（初回を除く。`0` で無効） |
| `WithRetryInterval` | 再試行までの待機時間（初期値・上限） | `500ms` / `4s` |

`WithExtractor` が差し替えるのは抽出エンジンだけで、HTTP の取得は差し替わりません。取得側を変えたいときは `WithHTTPClient` を使います。リクエストヘッダー（User-Agent など）も、`Do` の中で受け取った `*http.Request` を書き換えてから委譲すれば変更できます。

```go
type headerOverride struct{ inner reader.HTTPClient }

func (h headerOverride) Do(req *http.Request) (*http.Response, error) {
    req.Header.Set("User-Agent", "my-agent/1.0")
    return h.inner.Do(req)
}
```

-----

## 🔁 リトライ

HTTP(S) の取得は、一時的な失敗を指数バックオフでやり直します（既定で初回＋**2 回**、初期待機 **500ms**、上限 **4s**）。

| 失敗の種類 | 再試行 |
| :--- | :--- |
| 5xx / 408 / 429 | する（`Retry-After` があればその待ち時間に従う） |
| タイムアウトなど分類できない通信エラー | する |
| 4xx（429・408 を除く） | しない（繰り返しても結果が変わらないため） |
| レスポンスサイズ上限の超過 | しない |
| `context` のキャンセル・期限切れ | しない |

やり直しの判断を `reader` 側に置いているのは、`HTTPClient` の口が `Do` だけで、レスポンス 1 個からは「同じ GET をやり直してよいか」を決められないためです。既定の `httpkit.Client` も `Do` を直接呼ぶ経路にはリトライを掛けません。

`WithHTTPClient` に渡したクライアントが `IsHTTPRetryableError(error) bool` を持つ場合は、その判断が優先されます（エラーの型を知っているのはそれを返したクライアント自身のため）。自前のリトライ付きクライアントを注入するなら、二重に待たないよう `WithMaxRetries(0)` を添えてください。

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

**0. 文字コードを判定する** — BOM →`Content-Type` の `charset` →`<meta charset>` →本文のバイト列、の順に判定して UTF-8 に変換します。変換を挟まないと、パーサが入力を UTF-8 とみなすため非 UTF-8 のページがそのまま文字化けします。

**1. ノイズを落とす** — `script`、`style`、`form`、`nav`、`aside`、`noscript`、`template`、`[hidden]`、`[aria-hidden="true"]`、および広告・SNS・コメント欄まわりのクラス（`.ad-banner`、`.social-share`、`.comments` など）をページ全体から除去します。`noscript` / `template` はパーサからは中身がただのテキストに見えるため、落とさないと囲っている段落の本文に混ざります。

**2. 本文の範囲を決める** — `article` / `main` / `div[role=main]` / `#content` / `.entry-content` などに最初に一致した要素を本文とします。見つからない場合はページ全体を本文とみなし、そのときだけ `header` / `footer` / `.sidebar` も落とします（記事の内側の `header` は見出しを、`footer` は署名を含むことがあるため、常に落とすと本文が欠けます）。

**3. ブロック要素を順に拾う** — `p`、`h1`〜`h6`、`li`、`dt`、`dd`、`figcaption`、`blockquote`、`table`、`pre` を DOM の出現順に走査します。入れ子（`<li><p>…</p></li>` など）は一度だけ出力されます。`<br>` は空白として扱うため、前後の行が 1 語に融合しません。

| 要素 | 出力 |
| :--- | :--- |
| `title` | `【記事タイトル】 ` を付けて先頭に |
| `h1`〜`h6` | `## ` を付ける（**2 文字**以上のもの） |
| `p`, `blockquote` | **20 文字**以上のものだけ |
| `li`, `dt`, `dd`, `figcaption` | 長さを問わず出力（項目・定義語・キャプションは短くても意味を持つため） |
| `table` | `【表題】 ` 付きキャプション＋`セル \| セル` の行（空行は出力しない） |
| `pre` | ` ``` ` のコードフェンスで囲む |

しきい値はバイト数ではなく**文字数**で測ります。定数として `extract.MinParagraphLength` / `extract.MinHeadingLength` を公開しています。

-----

## 🛡️ URL の安全性 (SSRF 対策)

Web URL に対しては 2 段階の防御が働きます。

1. **取得前の検証** — `securenet.ValidateURL` が名前解決まで行い、プライベート / ループバック / リンクローカルなどの制限ネットワークを拒否します。
2. **接続直前の検証** — 既定の HTTP クライアント（`securenet.NewSafeHTTPClient`）が接続の直前にも IP を検証するため、DNS Rebinding も防げます。あわせて環境変数プロキシの無効化、リダイレクト追従の上限（10 回）、`https` → `http` ダウングレードの拒否も適用されます。

`gs://` / `s3://` はクラウド SDK が接続先を決めるため、検証は行われません。**スキームの振り分けが先で、`safeURL` は HTTP(S) の枝でだけ呼ばれます**（`WithSafeURLValidator` に渡した検証器も同様です）。`securenet.ValidateURL` は http/https 以外をスキーム違反として拒否するため、全スキームを通すとクラウドストレージが開けなくなります。

> **ローカルのテストサーバーへ向けたい場合**は既定のままでは遮断されます。`WithSafeURLValidator` と `WithHTTPClient` の両方を差し替えてください（片方だけでは接続直前の検証に引っかかります）。

-----

## 🛠️ 主要な依存関係 (Dependencies)

| モジュール | 役割 |
| :--- | :--- |
| [`github.com/PuerkitoBio/goquery`](https://github.com/PuerkitoBio/goquery) | `extract` の DOM 走査 |
| [`github.com/andybalholm/cascadia`](https://github.com/andybalholm/cascadia) | CSS セレクタの事前コンパイル（goquery の内部エンジン） |
| [`github.com/shouni/go-http-kit`](https://github.com/shouni/go-http-kit) | 既定の HTTP クライアントとレスポンス処理（25MB 上限）、取得のリトライ（`retry`） |
| [`github.com/shouni/go-remote-io`](https://github.com/shouni/go-remote-io) | GCS/S3 の I/O 抽象化。スキームの解釈もこちらに合わせています |
| [`github.com/shouni/netarmor`](https://github.com/shouni/netarmor) | URL・接続先 IP の安全性検証（SSRF 対策） |
| [`golang.org/x/net`](https://pkg.go.dev/golang.org/x/net) | `html` パッケージ（テキストノード走査）と `html/charset`（文字コード判定） |

-----

## 📌 v1.4.0 での変更

API の互換は保っています（追加のみ）。ただし**同じページでも出力と挙動が変わります**。

### 追加

| 追加 | 内容 |
| :--- | :--- |
| `extract.TextWithContentType` | `Content-Type` を判定材料に加えた `Text` |
| `Engine.ExtractWithContentType` | 同上（`reader.ContentTypeExtractor` を満たします） |
| `reader.ContentTypeExtractor` | 抽出器が `Content-Type` を受け取れることを表す追加インターフェース |
| `reader.RetryClassifier` | HTTP クライアントが再試行可否を判断できることを表す追加インターフェース |
| `(*UniversalReader).ReadAll` | 開いて読み切って閉じるまでを行う |
| `WithMaxRetries` / `WithRetryInterval` | リトライの調整 |

### 挙動の変化

* **非 UTF-8 のページが文字化けしなくなりました。** 以前は入力を常に UTF-8 とみなしていたため、Shift_JIS / EUC-JP のページが壊れていました。
* **一時的な失敗をやり直すようになりました。** 既定で初回＋2 回。失敗が続くと `Open` が返るまで最大 1 秒ほど余分にかかります。今までどおり 1 度で諦めるなら `WithMaxRetries(0)` を渡してください。
* **URL 安全性検証がスキームの振り分けの後になりました。** `gs://` / `s3://` には掛かりません（`securenet` が非 HTTP スキームを拒否するようになったため、掛けるとクラウドストレージが開けなくなります）。`WithSafeURLValidator` の検証器も HTTP(S) でだけ呼ばれます。
* **`Close` 後の `Open` が、`https://` でも `ErrClosed` を返すようになりました。** 以前は解放するものが無い HTTP だけ素通りしていて、ドキュメントの「`Close` は終端」と食い違っていました。
* `<br>` の前後が空白で区切られるようになりました（`行1<br>行2` が `行1行2` になっていました）。
* `noscript` / `template` / `[hidden]` / `[aria-hidden="true"]` の中身が本文に混ざらなくなりました。
* `dt` / `dd` / `figcaption` を本文として拾うようになりました（以前はどのブロック要素にも該当せず、丸ごと落ちていました）。
* 複数行にまたがる `<title>` が 1 行に整形されるようになりました。

抽出のベンチマーク（100 セクションの記事）は文字コード判定とセレクタ追加のぶん約 10% 遅くなっています。

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

`go-web-exact` の `scraper` / `runner` / `builder`（並列取得・レート制限・HTML ワーカープール）は利用者がいなかったため引き継いでいません。必要になったら同リポジトリの `v2.5.2` タグから取り出してください。

-----

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
