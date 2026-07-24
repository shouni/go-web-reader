# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Go Web Reader is a Go library + CLI that reads content from a URI regardless of backend — `https://`, `gs://`, and `s3://` are all handled behind a single `Open(ctx, uri)` call. The public API is `pkg/reader`; everything under `internal/` wires that library into the `go-web-reader` CLI binary.

## Commands

```bash
go build ./...                       # build everything
go vet ./...                         # vet
test -z "$(gofmt -l .)"              # format check (CI fails on any diff)
go test -race ./...                  # full test suite, as run in CI
go test -race ./pkg/reader/...       # single package
go test -race -run TestName ./...    # single test
golangci-lint run                    # lint (mirrors .golangci.yml: errcheck, govet, ineffassign, staticcheck, unused, gocritic, revive)
govulncheck ./...                    # vulnerability scan (also runs in CI)

go run . read --uri https://example.com/article
go run . read --uri gs://bucket/path/to/file.txt
go run . read --uri s3://bucket/path/to/file.txt
```

CI (`.github/workflows/ci.yml`) runs build/vet/gofmt/`go test -race`, golangci-lint, and govulncheck on every push/PR to `main` and `develop`.

## Architecture

### Layering

```
cmd/            Cobra CLI (root.go registers --uri flag + PreRunE validation; read.go runs the read subcommand)
internal/builder    DI: turns a *config.Config into a fully-wired *app.Container
internal/app        Container holds Config, the Pipeline, and a slice of io.Closer for cleanup
internal/pipeline   Business logic: depends only on the ContentReader interface, not on pkg/reader directly
internal/domain     The Pipeline port (interface) that internal/app depends on
internal/config     Config struct (SourceURL) + Normalize/Validate
internal/closeutil  closeutil.Join(...) — runs multiple closers, merges errors with errors.Join
pkg/reader          Public library: UniversalReader, the actual scheme-dispatching implementation
```

Dependency direction: `cmd` → `builder` → `app`/`pipeline` → `domain` (interfaces only). `internal/pipeline` never imports `pkg/reader` directly — it depends on its own local `ContentReader` interface, and `builder` supplies a `*pkgreader.UniversalReader` that satisfies it. This is what lets `pipeline_test.go` and `container_test.go` run without any real network/storage clients.

### pkg/reader (the actual engine)

- `reader.New(opts ...Option)` does a *lightweight* init only. GCS/S3 clients are lazily constructed on first `Open()` call for that scheme, cached in `storageReaderCache`, and released together on `Close()` via `closeutil.Join`.
- `Open(ctx, uri)` dispatches on scheme: `http(s)://` → `http.go`, `gs://`/`s3://` (checked via `remoteio.IsGCSURI`/`IsS3URI`) → `storage.go`. Every URI is passed through `securenet.IsSafeURL` before anything else runs.
- `http.go`: fetches via `HTTPClient` (defaults to `httpkit.New`), then branches on the response `Content-Type`. `text/html`/`application/xhtml+xml` go through the `go-web-exact` extractor (`openExtractedHTML`) to strip boilerplate and return body text only; `text/plain`/`text/markdown`/`text/x-markdown` are streamed back unmodified; anything else is an error. `fallbackMediaType` handles malformed Content-Type headers by matching against the known supported list.
- `storage.go`: GCS/S3 readers are built from `remoteio.IOFactory` and cached per-scheme in `UniversalReader` (guarded by `r.mu`); the underlying `remoteio.Reader`/`io.Closer` pair is reused across calls until `Close()`.
- `options.go` exposes `With*` functional options (`WithExtractor`, `WithHTTPClient`, `WithSafeURLValidator`, `WithGCSFactory`, `WithS3Factory`) — this is the seam tests and embedding applications use to swap out real network/cloud dependencies for fakes.

### Resource cleanup convention

Anything that opens external resources (HTTP client, GCS/S3 factories, the reader itself) is collected as an `io.Closer` and released through `closeutil.Join`, which runs every closer and merges errors with `errors.Join` rather than stopping at the first failure or silently dropping errors. Follow this pattern for any new resource that needs cleanup — don't `defer resource.Close()` and ignore the error; register it as a closer instead (see `internal/app.Container.Closers` and `UniversalReader.Close`).

## Key dependencies

- [`go-web-exact`](https://github.com/shouni/go-web-exact) — the main-content extraction engine used for HTML.
- [`go-remote-io`](https://github.com/shouni/go-remote-io) — GCS/S3 abstraction (`remoteio.IOFactory`, `IsGCSURI`/`IsS3URI`).
- `go-http-kit`, `netarmor` (URL safety validation via `securenet.IsSafeURL`), `clibase` (CLI bootstrapping used in `cmd/root.go`).
