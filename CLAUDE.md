# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Go Web Reader is a Go **library** (no CLI, no `main`) that reads content from a URI regardless of backend — `https://`, `gs://`, and `s3://` all go through one `Open(ctx, uri)`. Two public packages:

- `reader` — scheme dispatch, Content-Type handling, fetch retry, GCS/S3 lazy init, DI options.
- `extract` — HTML main-content extraction (charset detection included). Usable on its own.

`extract` は 3 ファイルに分かれています。`extract.go` が公開 API と抽出の流れ、`selectors.go` がセレクタとタグ集合の定義、`text.go` がノードを歩いてテキストを組み立てる部分です。

Dependency direction is one-way: `reader` → `extract`. Nothing imports `reader`.

## Commands

```bash
go build ./...                       # build everything
go vet ./...                         # vet
test -z "$(gofmt -l .)"              # format check (CI fails on any diff)
go test -race ./...                  # full test suite, as run in CI
go test -race ./reader/...           # single package
go test -race -run TestName ./...    # single test
golangci-lint run                    # lint (.golangci.yml: errcheck, govet, ineffassign, staticcheck, unused, gocritic, revive)
govulncheck ./...                    # vulnerability scan (also runs in CI)
```

CI (`.github/workflows/ci.yml`) runs build/vet/gofmt/`go test -race`, golangci-lint, and govulncheck on every push/PR to `main` and `develop`.

## Design decisions

Per-function rationale lives in the doc comments; this section covers only what the source cannot say from inside a single file.

### Interfaces live with their consumer

There is no shared `ports`/`types` package. `reader` declares the `HTTPClient` and `Extractor` interfaces it consumes; `extract` takes a plain `io.Reader` and names no type from `reader`. That is what keeps the dependency one-way — a shared interface package would let the two entangle. `extract.Engine` satisfies `reader.Extractor` structurally.

### Charset decoding lives in extract, and only there

`extract` は入力を UTF-8 に直してから解析します（`golang.org/x/net/html/charset`）。goquery / `x/net/html` は入力を UTF-8 とみなすため、これが無いと Shift_JIS / EUC-JP のページが丸ごと文字化けします。

**変換を挟む箇所は 1 つだけです。** UTF-8 に直したバイト列をもう一度 `<meta charset>` の宣言（Shift_JIS）で解釈し直せば壊れるため、`reader` 側でも変換する形にはできません。`reader` が持つ `Content-Type` は `ContentTypeExtractor`（`ExtractWithContentType`）を通して**判定材料として渡すだけ**で、変換はしません。この追加インターフェースは `WithExtractor` とは別の口ではなく、同じ口に渡された抽出器が持てる追加の能力です。

代償として `extract` は `x/text` のエンコーディング表を引き込みます（`x/net/html/charset` の依存）。日本語のページを読むライブラリでこれは払う価値のあるコストと判断していますが、`extract` の依存を「goquery・cascadia・`x/net/html`・標準ライブラリだけ」に保つ方針の唯一の例外です。

### extract does no I/O

`extract.Text(ctx, io.Reader)` is the whole engine; `extract.Engine` is an empty struct that forwards to it, existing only so `WithExtractor` has an interface value to accept. It is deliberately *not* named `Extractor`: that name belongs to the `reader` interface, and having both would put two different `Extractor` types in one codebase. The zero value works, so there is no constructor and no init error — which in turn is why `reader.New` returns no error.

This is a deliberate narrowing from the `go-web-exact` version this was absorbed from, which held a `Fetcher` purely to serve a `FetchAndExtractText(ctx, url)` convenience method. Fetching belongs to `reader`. **Do not reintroduce network access into `extract`.**

### One HTTP seam, not two

`WithHTTPClient` is the only way to change how fetching happens. There used to be a second seam — a `Fetcher` interface plus `WithFetcher` that replaced the whole fetch step — and it was removed because it had no users and no capability of its own: a custom `HTTPClient.Do` can rewrite the `*http.Request` (headers included) before delegating, which covers the case `WithFetcher` was there for. Removing it also let every default move into `newOptions`; the `Fetcher` default could not live there because it depended on `cfg.httpClient`.

Don't reintroduce it without a concrete requirement that `WithHTTPClient` genuinely cannot serve. Note that `httpkit.HandleResponse` — and therefore the 25MB response cap — is applied by `fetchOnce` outside the client, so a swapped client cannot lose it; a `Fetcher` seam could.

### The URL safety check runs after scheme dispatch, and only for HTTP(S)

`Open` はスキームを振り分けてから、HTTP(S) の枝の中でだけ `safeURL` を呼びます。順序を逆にすると **GCS/S3 が一切開けません**: netarmor v1.3.0 の `securenet.ValidateURL` は http/https 以外を `ErrDisallowedScheme` で拒否します（v1.2.3 は素通りさせていました）。

順序の問題である以前に、検証の意味がスキームによって違います。`SafeURLValidator` は「自分でダイヤルする相手が安全か」を名前解決込みで見る SSRF 対策で、接続先をクラウド SDK が決める `gs://` / `s3://` には掛ける相手がいません。

`WithSafeURLValidator` に渡した検証器も HTTP(S) でしか呼ばれません。ストレージ側の URI を弾きたい場合の口ではない、ということです。`reader_test.go` の `TestOpenStorageWithDefaultURLValidator` が、**既定の検証器を差し替えないまま** `gs://` / `s3://` を開ける唯一のテストです。他のテストは `newTestReader` が検証器を no-op に差し替えるため、ここが壊れても気づきません。消さないでください。

### Retry belongs to reader, not to the HTTP seam

`HTTPClient` の口は `Do` だけです。レスポンス 1 個を受け取るだけの `Do` からは「同じ GET をやり直してよいか」を決められないため、リトライは `fetchBytes` が `netarmor/retry` で掛けます。既定の `httpkit.Client` も `Do` を直接呼ぶ経路にはリトライを掛けない（`DoRequest` / `FetchBytes` 経由だけ）ので、ここを持たないと既定構成でも一度も再試行されません。

`httpkit.FetchBytes` に委譲しない理由は、あれが自前でリクエストを組み立てるためです。`newHTTPRequest` の `Accept` / `Sec-Fetch-*` / `Upgrade-Insecure-Requests` は httpkit が付けないぶん失われます。

再試行の可否は、クライアントが `RetryClassifier`（`IsHTTPRetryableError`）を満たすならそちらに委ねます。エラーの型を知っているのはそれを返したクライアントなので、既定構成ではリトライ対象の定義が httpkit と二重管理になりません。満たさないクライアント向けのフォールバック判定が `shouldRetryFetch` の後半です。

待ち時間の既定値は httpkit（初期 5 秒・最大 30 秒）より短くしています。`Open` は呼び出し側を待たせる同期 API だからです。

### Two selector lists, removed at different times

`extract` removes `noiseSelectors` (script/style/form/nav/aside/ad-ish classes) from the whole document *before* choosing the main content, so an `<article>` nested inside an `<aside>` cannot be mistaken for the body. `pageFrameSelectors` (header/footer/.sidebar) is removed *only* on the fallback path, because inside a real article a `<header>` usually holds the `<h1>` and a `<footer>` the byline — dropping those unconditionally loses body text.

Anything that adds a selector must decide which of the two lists it belongs to.

### Nested blocks are emitted once

`FindMatcher(blockMatcher)` visits a parent and its matching descendants both (goquery dedupes *nodes*, not nested text). `writeOwnText` is what prevents `<li><p>…</p></li>` from printing the same sentence twice: it refuses to descend into any child whose tag is in `blockTagSet`, because that child gets its own visit.

`blockMatcher` and `blockTagSet` are both derived from the `blockTags` slice, so extending the element list in that one place keeps the two in step. Don't hardcode either of them. 長さのしきい値を免除する要素も同じ形で `shortTags` から `shortTagSet` を導出しています。

`<br>` はブロック要素ではないので走査対象にはなりませんが、`writeOwnText` で空白に置き換えます。置き換えないと `行1<br>行2` の 2 つのテキストノードが直結して 1 語になります。

### Selectors are compiled once, and tag checks skip CSS entirely

goquery's string-taking `Find`/`Is` call `cascadia.Compile` on **every** call — there is no cache. That is fine for a once-per-document query and wasteful for a per-node one, so every selector here is a package-level `cascadia.MustCompile` used through `FindMatcher`/`IsMatcher`.

Beyond that, single-tag tests don't need the CSS machinery at all: `tagName` reads `html.Node.Data` directly, and `writeOwnText` walks `FirstChild`/`NextSibling` rather than `Contents().Each`, which allocates a `Selection` per child. Together those cut ~40% of the run time and ~68% of the allocations on a 100-section article (`go test -bench BenchmarkText ./extract/`). Keep new per-node checks off the string API.

文字コード判定の追加と `noiseSelectors` / `blockTags` の拡張で、同じベンチマークが 716µs → 789µs（+10%、内訳はおよそ半々）になっています。**速度を理由にこのどちらかを戻さないでください** — 落ちるのは出力の正しさの側です。経路ごとのコストは `BenchmarkTextCharset` で測れます。

### Length thresholds are counted in runes

`len()` would measure bytes, making the thresholds effectively one third for Japanese text and letting navigation fragments through. `MinHeadingLength` is 2 rather than 3 so that two-character headings (`概要`) survive. `<li>` is exempt from the paragraph threshold — list items are legitimately short.

## Conventions

- **Cleanup**: `Close` runs *every* closer and merges failures with `errors.Join`; it never stops at the first error or drops one. Don't `defer resource.Close()` and ignore the result.
- **Close is terminal for every scheme**: `UniversalReader.closed` が印です。スキームごとのキャッシュにも同じ印はありますが、HTTP には解放するものが無いぶん印が付かず、それだけだと `Close` 後も `https://` が読めてしまいます。
- **Content-Type support** is the `mediaKinds` table in `reader/http.go` alone — `classifyMediaType` drives both the dispatch switch and the malformed-header fallback, so adding a type means editing that table and nothing else.
- **Scheme parsing** is borrowed from `remoteio.SchemePrefix` rather than reimplemented. gs/s3 でこれが必須なのは、`reader` が URI を**そのまま** `remoteio.Reader.Open` に渡し、向こう側が再度パースするためです。「どこからがスキームか」の解釈がずれれば実バグになります。HTTP(S) も同じ関数を通していますが、理由は違います — http URI を go-remote-io は見ないので、**合わせる相手はいません**。揃えているのは振り分けを 1 系統にするためで、副産物として `strings.HasPrefix` を手書きする際の罠（`"://"` を付け忘れると `"httpfoo://"` まで HTTP 扱いになる）が消えます。スキーム名そのものは `securenet.SchemeHTTP` / `SchemeHTTPS` から取ります。Adding a scheme means one more entry in the `storages` map in `reader.New`.
- **大文字スキームは未対応**: `SchemePrefix` も `strings.HasPrefix` も大小を区別するため、`HTTPS://example.com` は「未対応のURIスキームです」になります（`url.Parse` と違い正規化しません）。対応するなら HTTP とストレージの両方を揃えて直してください。片側だけ直すと非対称になります。
- **Dispatch is not a security predicate**: 振り分けに `securenet.IsSecureServiceURL` や `ValidateURL` の成否を使わないでください。前者は平文 HTTP を localhost 等にしか許さないので `http://` が丸ごと未対応スキームになります。後者は (1) 実際に走るのは注入された `r.safeURL` なので、利用者の検証器が「どのバックエンドが処理するか」を左右してしまい、(2) 分岐条件が netarmor のセキュリティ方針そのものになる（v1.2.3 → v1.3.0 で実際に変わった）うえ、(3) 名前解決を伴うため振り分けが I/O になります。

## Key dependencies

- `github.com/PuerkitoBio/goquery` — DOM traversal for `extract`.
- `github.com/andybalholm/cascadia` — goquery's own selector engine, used directly to precompile selectors (see below).
- `github.com/shouni/go-http-kit` — the default client (`httpkit.New`) and `HandleResponse`, which is where the 25MB response cap comes from.
- `github.com/shouni/go-remote-io` — GCS/S3 abstraction (`remoteio.IOFactory`, `remoteio.Reader`, `SchemePrefix`, `PrefixGCS`/`PrefixS3`, `gcs.New`/`s3.New`).
- `github.com/shouni/netarmor` — `securenet.ValidateURL`, the scheme constants, and `retry.RunValue` for the fetch retry loop.
- `golang.org/x/net` — `html` node types (`extract`'s text walk) and `html/charset` (charset detection).

`extract` depends on goquery, cascadia, `x/net/html`, `x/net/html/charset` and the stdlib — and the first two are the same dependency, since goquery already requires cascadia. `html/charset` is the one entry that pulls something heavier in (`x/text`'s encoding tables); the rationale is in **Charset decoding lives in extract, and only there** above. Whitespace normalization used to come from `go-utils/text.NormalizeText`; it moved here as `normalizeSpace` because a one-line `strings.Join(strings.Fields(s), " ")` dragged gomoji and uniseg (5.6MB of emoji and grapheme tables this repo never calls) into the build. That package has since been deleted from go-utils for the same reason, so there is nothing to go back to. Keep helpers local unless one earns its dependency.

## History

`extract` was absorbed from [`go-web-exact`](https://github.com/shouni/go-web-exact) v2.5.2, now retired. Its `scraper`/`runner`/`builder` packages (parallel fetching with rate limiting, retries, HTML worker pool) were **not** brought over — they had no users. If bulk scraping is needed again, recover them from that repo's `v2.5.2` tag rather than rewriting, and note that `scraper` fetched without going through `SafeURLValidator`: anything reintroduced here must go through it.
