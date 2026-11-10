# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Go Web Reader is a Go **library** (no CLI, no `main`) that reads content from a URI regardless of backend — `https://`, `http://`, `gs://`, and `s3://` all go through one `Open(ctx, uri)`. Two public packages:

- `reader` — scheme dispatch, Content-Type handling, fetch retry, GCS/S3 lazy init, DI options.
  `reader.go` holds the public API and scheme dispatch, `options.go` the interfaces and `With*`, `fetch.go` HTTP fetching and retry, `http.go` Content-Type dispatch, and `storage.go` the lazy GCS/S3 init.
- `extract` — HTML main-content extraction (charset detection included). Usable on its own.
  `extract.go` holds the public API and the extraction flow, `selectors.go` the selectors and tag sets, `text.go` text assembly for block elements, and `table.go` table flattening.

Tests sit in a `_test.go` matching each implementation file 1:1. Dependency direction is one-way: `reader` → `extract`. Nothing imports `reader`.

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

`extract` converts its input to UTF-8 before parsing (`golang.org/x/net/html/charset`). **There is exactly one place where the conversion happens** — bytes already converted to UTF-8 would break if reinterpreted by their `<meta charset>` declaration, so `reader` cannot convert. `reader` only passes the `Content-Type` along through `ContentTypeExtractor` as a hint.

The price is that `extract` pulls in `x/text`'s encoding tables. It is the one exception to keeping `extract`'s dependencies to "goquery, cascadia, `x/net/html` and the standard library", judged worth paying for a library that reads Japanese pages.

### extract does no I/O

`extract.Text(ctx, io.Reader)` is the whole engine; `extract.Engine` is an empty struct that forwards to it. The zero value works, so there is no constructor and no init error — which is why `reader.New` returns no error. **Do not reintroduce network access into `extract`** (the `go-web-exact` version it came from had a `Fetcher`; fetching belongs to `reader`).

### One HTTP seam, not two

`WithHTTPClient` is the only way to change how fetching happens. A `Fetcher`/`WithFetcher` seam that replaced the whole fetch step was removed: a custom `HTTPClient.Do` can rewrite the `*http.Request` before delegating, which covers every case it served. Don't reintroduce it without a concrete requirement `WithHTTPClient` cannot serve. Note that the 25MB response cap (`httpkit.HandleResponse`) is applied by `fetchOnce` outside the client, so a swapped client cannot lose it; a `Fetcher` seam could.

### The URL safety check runs after scheme dispatch, and only for HTTP(S)

The reason is in the comment on `Open`. What needs saying here is that the ordering guarantee depends on a test: `newTestReader` in `reader_test.go` / `storage_test.go` swaps the validator for a no-op, so **only `TestOpenStorageWithDefaultURLValidator` checks that `gs://` / `s3://` open with the default validator in place**. Do not delete it.

### Retry belongs to reader, not to the HTTP seam

`ReadAll` has no size limit of its own: HTTP stops at `HandleResponse`'s 25MB, but `gs://` / `s3://` hand the stream straight through. `ReadAllLimit` (fail with `ErrTooLarge`) and `ReadText` (truncate at a UTF-8 boundary, report it) exist because two services were hand-rolling the same read-one-byte-more-than-the-limit dance around `Open`. Neither logs; a truncation is the caller's to record.

The `HTTPClient` seam is only `Do`, so retry is applied by `fetchBytes` with `go-http-kit/retry`. The default `httpkit.Client` does not retry `Do` either, so without this not even the default configuration would ever retry. Whether to retry is delegated to the client when it satisfies `RetryClassifier`; the second half of `shouldRetryFetch` is the fallback for clients that do not. The default wait times are shorter than httpkit's (5 s initial, 30 s max) — because `Open` is a synchronous API. For the same reason a `Retry-After` instruction is also capped at `maxInterval` (`retry.WithMaxRetryAfter`). Backoff waits for the instructed value as is, and `MaxInterval` does not bound it, so without this a user-supplied URL whose server merely returns `Retry-After: 3600` would keep `Open` from returning until the ctx deadline. Beyond the cap it gives up without waiting, with `retry.ErrRetryAfterTooLong`.

### Two selector lists, removed at different times

`noiseSelectors` is removed from the whole document *before* choosing the main content, so an `<article>` nested inside an `<aside>` cannot be mistaken for the body. `pageFrameSelectors` (header/footer/.sidebar) is removed *only* on the fallback path, because inside a real article a `<header>` holds the `<h1>` and a `<footer>` the byline. Anything that adds a selector must decide which list it belongs to.

### Nested blocks are emitted once

`FindMatcher(blockMatcher)` visits a parent and its matching descendants both (goquery dedupes *nodes*, not nested text). `ownText` refuses to descend into any child whose tag is in `blockTagSet`, because that child gets its own visit. `blockMatcher` and `blockTagSet` are both derived from `blockTags`; `shortTagSet` and `flattenBoundarySet` follow the same pattern. Don't hardcode any of them.

**`div` is not in `blockTags`, and the fallback for `<div>`-written bodies is deliberately narrow.** Adding `div` to the block list would make every wrapper a paragraph and double-emit everything under it. Instead, `collectContainerParagraphs` runs only when the block scan produced no body at all: it takes leaf containers (`div`/`section`/`article` with no block or container descendants), splits their text at `<br>`, and applies `MinParagraphLength` like `<p>`. A page with even one real block keeps only the blocks — a mixed page's wrapper asides are not body. `document_with_div_and_br_body` / `document_with_p_ignores_div_text` pin both halves.

Tables are the reverse: block elements inside a table are skipped by the scan (`insideTable`), and `processTable` flattens them into cell text. Emitting the `<p>` in `<td><p>…</p></td>` on its own would lose the row's context, and a short one would vanish under the threshold.

### Selectors are compiled once, and tag checks skip CSS entirely

goquery's string-taking `Find`/`Is` call `cascadia.Compile` on **every** call. Every selector here is a package-level `cascadia.MustCompile`, and single-tag tests read `html.Node.Data` directly instead. Keep new per-node checks off the string API. Charset detection and the extensions to `noiseSelectors` / `blockTags` made `BenchmarkText/sections=100` about 10% slower. **Do not revert them for speed** — what would suffer is the correctness of the output.

### Length thresholds are counted in runes

`len()` would measure bytes, making the thresholds effectively one third for Japanese text. `MinHeadingLength` is 2 so that `概要` survives. `<li>`/`<dt>`/`<dd>`/`<figcaption>` and table cells are exempt from the paragraph threshold.

## Conventions

- **Language**: comments, doc comments, error/log/panic messages and test assertion messages are English (moved 2026-11-10; do not mix Japanese back in). Error strings follow Go style: lowercase, no trailing punctuation (ST1005). Japanese stays only where it is data — HTML/text fixtures, runtime-compared strings, encoding test data. Subtest names (`t.Run` names and table `name` fields) are still Japanese for now. `README.md` is English and `README.ja.md` is its Japanese version; update the two together.
- **Cleanup**: `Close` runs *every* closer and merges failures with `errors.Join`. Don't `defer resource.Close()` and ignore the result.
- **Close is terminal for every scheme**, including HTTP — `UniversalReader.closed` is the marker.
- **Content-Type support** is the `mediaKinds` table in `reader/http.go`. Adding a type means editing that table and the README's Supported Content-Types list (in both `README.md` and `README.ja.md`; the only user-facing copy — `mediaKinds` is unexported). `resolveMediaType`'s rescue path — for headers `mime.ParseMediaType` rejects but real servers still send, like an unclosed quote around `charset` — only accepts a well-formed `type/subtype` token (`isMediaTypeToken`). Without that check `classifyMediaType`'s deliberately open `image/` prefix match let `"image/ 0"` through as an image, and handed back a string that is not a usable media type. A fuzz target found it.
- **Both external-input surfaces are fuzzed** (`extract#FuzzText`, `reader#FuzzResolveMediaType`, 60s each in CI): arbitrary bytes from a user-supplied URL meet a DOM walk in one and hand-rolled header parsing in the other. `FuzzText` pins the return-value contract (an error means empty text and `hasBody` false; `hasBody` true means non-blank text) **and determinism** — the extractor walks maps in places, and an order leak there would change a notification's contents between runs.
- **Scheme parsing** uses `remoteio.Scheme` for every scheme. For gs/s3 it is required, because the URI is handed to `remoteio.Reader.Open` as is; HTTP(S) follows it so that dispatch has a single path. Scheme names come from `securenet.SchemeHTTP` / `SchemeHTTPS`, and the keys are names without the separator (`"gs"`). Adding a scheme means one more entry in the `storages` map in `reader.New`.
- **Upper-case schemes are not supported** (`HTTPS://`). If you add support, fix HTTP and storage together.
- **Dispatch is not a security predicate**: do not use `securenet`'s checks for dispatch (the reason is in the comment on `Open`). On top of that, they resolve names, which would turn dispatch into I/O.
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
