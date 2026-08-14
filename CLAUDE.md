# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Go Web Reader is a Go **library** (no CLI, no `main`) that reads content from a URI regardless of backend — `https://`, `gs://`, and `s3://` all go through one `Open(ctx, uri)`. Two public packages:

- `reader` — scheme dispatch, Content-Type handling, GCS/S3 lazy init, DI options.
- `extract` — HTML main-content extraction. Usable on its own.

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

### extract does no I/O

`extract.Text(ctx, io.Reader)` is the whole engine; `extract.Engine` is an empty struct that forwards to it, existing only so `WithExtractor` has an interface value to accept. It is deliberately *not* named `Extractor`: that name belongs to the `reader` interface, and having both would put two different `Extractor` types in one codebase. The zero value works, so there is no constructor and no init error — which in turn is why `reader.New` returns no error.

This is a deliberate narrowing from the `go-web-exact` version this was absorbed from, which held a `Fetcher` purely to serve a `FetchAndExtractText(ctx, url)` convenience method. Fetching belongs to `reader`. **Do not reintroduce network access into `extract`.**

### One HTTP seam, not two

`WithHTTPClient` is the only way to change how fetching happens. There used to be a second seam — a `Fetcher` interface plus `WithFetcher` that replaced the whole fetch step — and it was removed because it had no users and no capability of its own: a custom `HTTPClient.Do` can rewrite the `*http.Request` (headers included) before delegating, which covers the case `WithFetcher` was there for. Removing it also let every default move into `newOptions`; the `Fetcher` default could not live there because it depended on `cfg.httpClient`.

Don't reintroduce it without a concrete requirement that `WithHTTPClient` genuinely cannot serve. Note that `httpkit.HandleResponse` — and therefore the 25MB response cap — is applied by `fetchBytes` outside the client, so a swapped client cannot lose it; a `Fetcher` seam could.

### Two selector lists, removed at different times

`extract` removes `noiseSelectors` (script/style/form/nav/aside/ad-ish classes) from the whole document *before* choosing the main content, so an `<article>` nested inside an `<aside>` cannot be mistaken for the body. `pageFrameSelectors` (header/footer/.sidebar) is removed *only* on the fallback path, because inside a real article a `<header>` usually holds the `<h1>` and a `<footer>` the byline — dropping those unconditionally loses body text.

Anything that adds a selector must decide which of the two lists it belongs to.

### Nested blocks are emitted once

`Find(blockSelectors)` visits a parent and its matching descendants both (goquery dedupes *nodes*, not nested text). `ownText` is what prevents `<li><p>…</p></li>` from printing the same sentence twice: it refuses to descend into any child that is itself in `blockSelectors`, because that child gets its own visit. If you extend `blockSelectors`, this keeps working automatically — the two use the same constant on purpose.

### Length thresholds are counted in runes

`len()` would measure bytes, making the thresholds effectively one third for Japanese text and letting navigation fragments through. `MinHeadingLength` is 2 rather than 3 so that two-character headings (`概要`) survive. `<li>` is exempt from the paragraph threshold — list items are legitimately short.

## Conventions

- **Cleanup**: `Close` runs *every* closer and merges failures with `errors.Join`; it never stops at the first error or drops one. Don't `defer resource.Close()` and ignore the result.
- **Content-Type support** is the `mediaKinds` table in `reader/http.go` alone — `classifyMediaType` drives both the dispatch switch and the malformed-header fallback, so adding a type means editing that table and nothing else.
- **Scheme parsing** is borrowed from `remoteio.SchemePrefix` rather than reimplemented, so the two libraries cannot disagree on where a scheme ends. Adding a scheme means one more entry in the `storages` map in `reader.New`.

## Key dependencies

Five direct requires, listed in `go.mod` order:

- `github.com/PuerkitoBio/goquery` — DOM traversal for `extract`.
- `github.com/shouni/go-http-kit` — the default client (`httpkit.New`) and `HandleResponse`, which is where the 25MB response cap comes from.
- `github.com/shouni/go-remote-io` — GCS/S3 abstraction (`remoteio.IOFactory`, `remoteio.Reader`, `SchemePrefix`, `PrefixGCS`/`PrefixS3`, `gcs.New`/`s3.New`).
- `github.com/shouni/netarmor` — `securenet.ValidateURL` and the scheme constants.
- `golang.org/x/net` — `html` node types, used by `extract`'s text walk.

`extract` deliberately depends on nothing but goquery, `x/net/html` and the stdlib. Whitespace normalization used to come from `go-utils/text.NormalizeText`, which is a one-line `strings.Join(strings.Fields(s), " ")` but drags gomoji and uniseg (5.6MB of emoji and grapheme tables this repo never calls) into the build. It now lives here as `normalizeSpace`. Keep it that way unless a helper earns its dependency.

## History

`extract` was absorbed from [`go-web-exact`](https://github.com/shouni/go-web-exact) v2.5.2, now retired. Its `scraper`/`runner`/`builder` packages (parallel fetching with rate limiting, retries, HTML worker pool) were **not** brought over — they had no users. If bulk scraping is needed again, recover them from that repo's `v2.5.2` tag rather than rewriting, and note that `scraper` fetched without going through `SafeURLValidator`: anything reintroduced here must go through it.
