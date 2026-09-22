# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Go Web Reader is a Go **library** (no CLI, no `main`) that reads content from a URI regardless of backend — `https://`, `http://`, `gs://`, and `s3://` all go through one `Open(ctx, uri)`. Two public packages:

- `reader` — scheme dispatch, Content-Type handling, fetch retry, GCS/S3 lazy init, DI options.
  `reader.go` が公開 API とスキーム振り分け、`options.go` がインターフェースと `With*`、`fetch.go` が HTTP の取得とリトライ、`http.go` が Content-Type の振り分け、`storage.go` が GCS/S3 の遅延初期化です。
- `extract` — HTML main-content extraction (charset detection included). Usable on its own.
  `extract.go` が公開 API と抽出の流れ、`selectors.go` がセレクタとタグ集合、`text.go` がブロック要素のテキスト組み立て、`table.go` が表の平坦化です。

テストは実装ファイルと 1:1 の `_test.go` に置きます。Dependency direction is one-way: `reader` → `extract`. Nothing imports `reader`.

## Commands

```bash
go build ./...                       # build everything
go vet ./...                         # vet
test -z "$(gofmt -l .)"              # format check (CI fails on any diff)
go test -race ./...                  # full test suite, as run in CI
go test -race ./reader/...           # single package
go test -race -run TestName ./...    # single test
go test -bench . -run ^$ ./extract/  # extraction benchmarks
golangci-lint run                    # lint (.golangci.yml: errcheck, govet, ineffassign, staticcheck, unused, gocritic, revive)
govulncheck ./...                    # vulnerability scan (also runs in CI)
```

CI (`.github/workflows/ci.yml`) runs build/vet/gofmt/`go test -race`, golangci-lint, and govulncheck on every push/PR to `main` and `develop`.

## Design decisions

Per-function rationale lives in the doc comments; this section covers only what the source cannot say from inside a single file.

### Interfaces live with their consumer

There is no shared `ports`/`types` package. `reader` declares the `HTTPClient` and `Extractor` interfaces it consumes; `extract` takes a plain `io.Reader` and names no type from `reader`. A shared interface package would let the two entangle. `extract.Engine` satisfies `reader.Extractor` structurally. `Engine` is deliberately *not* named `Extractor` — that name belongs to the `reader` interface.

### Charset decoding lives in extract, and only there

`extract` は入力を UTF-8 に直してから解析します（`golang.org/x/net/html/charset`）。**変換を挟む箇所は 1 つだけです** — UTF-8 に直したバイト列を再度 `<meta charset>` の宣言で解釈し直せば壊れるため、`reader` 側では変換できません。`reader` の `Content-Type` は `ContentTypeExtractor` を通して判定材料として渡すだけです。

代償として `extract` は `x/text` のエンコーディング表を引き込みます。`extract` の依存を「goquery・cascadia・`x/net/html`・標準ライブラリだけ」に保つ方針の唯一の例外で、日本語のページを読むライブラリとして払う価値があると判断しています。

### extract does no I/O

`extract.Text(ctx, io.Reader)` is the whole engine; `extract.Engine` is an empty struct that forwards to it. The zero value works, so there is no constructor and no init error — which is why `reader.New` returns no error. **Do not reintroduce network access into `extract`** (the `go-web-exact` version it came from had a `Fetcher`; fetching belongs to `reader`).

### One HTTP seam, not two

`WithHTTPClient` is the only way to change how fetching happens. A `Fetcher`/`WithFetcher` seam that replaced the whole fetch step was removed: a custom `HTTPClient.Do` can rewrite the `*http.Request` before delegating, which covers every case it served. Don't reintroduce it without a concrete requirement `WithHTTPClient` cannot serve. Note that the 25MB response cap (`httpkit.HandleResponse`) is applied by `fetchOnce` outside the client, so a swapped client cannot lose it; a `Fetcher` seam could.

### The URL safety check runs after scheme dispatch, and only for HTTP(S)

理由は `Open` のコメントにあります。ここで言うべきは順序の保証がテストに依存していることです: `reader_test.go` / `storage_test.go` の `newTestReader` は検証器を no-op に差し替えるため、**`TestOpenStorageWithDefaultURLValidator` だけが既定の検証器のまま `gs://` / `s3://` を開ける**ことを確かめています。消さないでください。

### Retry belongs to reader, not to the HTTP seam

`HTTPClient` の口は `Do` だけなので、リトライは `fetchBytes` が `go-http-kit/retry` で掛けます。既定の `httpkit.Client` も `Do` にはリトライを掛けないため、ここを持たないと既定構成でも一度も再試行されません。再試行の可否はクライアントが `RetryClassifier` を満たすならそちらに委ね、満たさないクライアント向けのフォールバックが `shouldRetryFetch` の後半です。待ち時間の既定値は httpkit（初期 5 秒・最大 30 秒）より短くしています — `Open` は同期 API だからです。同じ理由で `Retry-After` の指示にも `maxInterval` を上限として掛けています（`retry.WithMaxRetryAfter`）。backoff は指示値をそのまま待ち、`MaxInterval` では抑えられないので、これが無いと利用者が入力した URL の相手が `Retry-After: 3600` を返しただけで `Open` が ctx の期限まで戻りません。上限を超えたら待たずに `retry.ErrRetryAfterTooLong` で打ち切ります。

### Two selector lists, removed at different times

`noiseSelectors` is removed from the whole document *before* choosing the main content, so an `<article>` nested inside an `<aside>` cannot be mistaken for the body. `pageFrameSelectors` (header/footer/.sidebar) is removed *only* on the fallback path, because inside a real article a `<header>` holds the `<h1>` and a `<footer>` the byline. Anything that adds a selector must decide which list it belongs to.

### Nested blocks are emitted once

`FindMatcher(blockMatcher)` visits a parent and its matching descendants both (goquery dedupes *nodes*, not nested text). `ownText` refuses to descend into any child whose tag is in `blockTagSet`, because that child gets its own visit. `blockMatcher` and `blockTagSet` are both derived from `blockTags`; `shortTagSet` and `flattenBoundarySet` follow the same pattern. Don't hardcode any of them.

表だけは逆向きです: 表の内側のブロック要素は走査で飛ばし（`insideTable`）、`processTable` がセルの文字列として平坦化します。`<td><p>…</p></td>` の `<p>` を個別に出すと行の文脈が失われ、短ければしきい値で消えるためです。

### Selectors are compiled once, and tag checks skip CSS entirely

goquery's string-taking `Find`/`Is` call `cascadia.Compile` on **every** call. Every selector here is a package-level `cascadia.MustCompile`, and single-tag tests read `html.Node.Data` directly instead. Keep new per-node checks off the string API. 文字コード判定と `noiseSelectors` / `blockTags` の拡張で `BenchmarkText/sections=100` は約 10% 遅くなりました。**速度を理由に戻さないでください** — 落ちるのは出力の正しさの側です。

### Length thresholds are counted in runes

`len()` would measure bytes, making the thresholds effectively one third for Japanese text. `MinHeadingLength` is 2 so that `概要` survives. `<li>`/`<dt>`/`<dd>`/`<figcaption>` and table cells are exempt from the paragraph threshold.

## Conventions

- **Cleanup**: `Close` runs *every* closer and merges failures with `errors.Join`. Don't `defer resource.Close()` and ignore the result.
- **Close is terminal for every scheme**, including HTTP — `UniversalReader.closed` が印です。
- **Content-Type support** is the `mediaKinds` table in `reader/http.go`. Adding a type means editing that table and the README's 対応 Content-Type list (the only user-facing copy — `mediaKinds` is unexported).
- **Scheme parsing** uses `remoteio.Scheme` for every scheme. gs/s3 では URI をそのまま `remoteio.Reader.Open` に渡すので必須で、HTTP(S) は振り分けを 1 系統にするために揃えています。スキーム名は `securenet.SchemeHTTP` / `SchemeHTTPS` から取り、キーは区切りを含まない名前 (`"gs"`) です。Adding a scheme means one more entry in the `storages` map in `reader.New`.
- **大文字スキームは未対応** (`HTTPS://`)。対応するなら HTTP とストレージの両方を揃えて直してください。
- **Dispatch is not a security predicate**: 振り分けに `securenet` の判定を使わないでください（理由は `Open` のコメント）。名前解決を伴うため振り分けが I/O になる、という点も加わります。
- **README.md follows `public-docs/docs/library-readme-convention.md`**: scope and traps only, one "first call" code example, signatures and defaults in godoc, reasoning here. No option/retry tables, no changelog.

## Key dependencies

- `goquery` + `cascadia` (goquery's own engine, used directly to precompile selectors) — `extract`.
- `go-http-kit` — default client, `HandleResponse` (the 25MB cap), `retry.RunValue`.
- `go-remote-io` — GCS/S3 abstraction and `Scheme`.
- `netarmor/securenet` — `ValidateURL` and the scheme constants.
- `x/net` — `html` node types and `html/charset`.

Keep helpers local unless one earns its dependency: `normalizeSpace` used to be `go-utils/text.NormalizeText`, which dragged 5.6MB of emoji/grapheme tables into the build for a one-liner. That package has since been deleted from go-utils.

## History

`extract` was absorbed from [`go-web-exact`](https://github.com/shouni/go-web-exact) v2.5.2 (retired); the import paths and names settled then (`pkg/reader` → `reader`, `ports.Extractor.ExtractText` → `reader.Extractor.Extract`, CLI dropped, `reader.New` stopped returning an error). Its `scraper`/`runner`/`builder` packages (parallel fetching, rate limiting, worker pool) were **not** brought over. If bulk scraping is needed again, recover them from that repo's `v2.5.2` tag rather than rewriting, and note that `scraper` bypassed `SafeURLValidator`: anything reintroduced here must go through it.
