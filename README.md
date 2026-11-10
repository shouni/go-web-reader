# 📖 Go Web Reader

[![CI](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-web-reader/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://go.dev/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-web-reader)](https://go.dev/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-web-reader)](https://github.com/shouni/go-web-reader/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-web-reader.svg)](https://pkg.go.dev/github.com/shouni/go-web-reader)

English | [日本語](README.ja.md)

## 🚀 About - Web main-content extraction and GCS / S3 reads through the same `Open(ctx, uri)`

**Go Web Reader** is a Go **library** that handles main-content extraction from web pages and reads from cloud
storage (GCS / S3) behind a single interface. Pass an `https://`, `http://`, `gs://` or `s3://` URI and get the
content back as an `io.ReadCloser`, without caring how the backend is reached.

Out of scope: **writing** (that is [go-remote-io](https://github.com/shouni/go-remote-io)'s job) and
**local files** (read them directly with the standard library).

### Where go-remote-io ends and this library begins

The sister library [go-remote-io](https://github.com/shouni/go-remote-io) also reads `gs://`, but it serves a
different stage. This table lives here only.

| | go-remote-io | go-web-reader |
| --- | --- | --- |
| Direction | Read and write (plus signed URLs and listing) | **Read only** |
| Targets | `gs://` / `s3://` / local | `https://` / `gs://` / `s3://` (**no local files**) |
| Role | **Where artifacts are stored** | **Where source material comes from** |
| HTML | Bytes as they are | **Main content only** (HTTP(S) only; HTML on `gs://` / `s3://` is returned as is) |

-----

## ✨ Features

* **Scheme-agnostic reads** — HTTP/HTTPS, GCS and S3 all go through the same `Open(ctx, uri)`. When reading
  the whole content is fine, there is `ReadAll`. GCS/S3 clients are not created until the first `Open` for that scheme.
* **Automatic switching by Content-Type** — HTML goes through main-content extraction, text and images are
  returned as they are, anything else is an error ([supported types](#-supported-content-types)).
* **Main-content extraction** — returns only the body text, with ads and navigation removed based on the DOM
  structure. Shift_JIS / EUC-JP pages are read too, with charset detection (traversal order, selectors and
  output shape are in [the `extract` godoc](https://pkg.go.dev/github.com/shouni/go-web-reader/extract)).
  `extract` does no network access at all and only parses the `io.Reader` it is given, so it can be used on its own.
* **Retry on transient failures** — 5xx responses and network errors are retried with exponential backoff
  (`WithMaxRetries` / `WithRetryInterval`).
* **SSRF protection** — besides validating the URL before fetching, the IP is validated again right before
  connecting ([URL safety](#-url-safety-ssrf-protection)).

-----

## 📦 Package Structure

```text
go-web-reader/
├── reader/     # [PUBLIC] the universal reader
│               #   scheme dispatch / Content-Type branching /
│               #   lazy GCS/S3 init / dependency-swapping options
└── extract/    # [PUBLIC] HTML main-content extraction engine (usable on its own)
```

Dependencies point one way: `reader` → `extract`.

-----

## 🚦 Usage

```go
import "github.com/shouni/go-web-reader/reader"

r := reader.New()
defer func() { _ = r.Close() }()

body, err := r.ReadAll(ctx, "https://example.com/article") // gs:// / s3:// are called the same way
```

**Only `gs://` / `s3://` are streamed.** HTTP(S) reads the whole response before returning, for main-content
extraction and retries, so piping the `io.ReadCloser` from `Open` through `io.Copy` saves no memory
(the cap is `go-http-kit`'s response size limit).

If you already have the HTML in hand, call `extract.Text` directly. If you also have the `Content-Type` header,
pass it to `extract.TextWithContentType` — for a Shift_JIS page that does not declare `<meta charset>`, the
header's `charset` is the only clue.

### Swapping dependencies (`Option`)

These are the seams for avoiding the real network and real cloud in tests or embedded use. The defaults are
`httpkit.New` with SSRF protection, `extract.Engine{}`, `securenet.ValidateURL` and `gcs.New` / `s3.New`.
The full list is on [pkg.go.dev](https://pkg.go.dev/github.com/shouni/go-web-reader/reader), and the traps of
swapping (duplicated retries, a size cap that cannot be removed, IP validation that remains even with a looser
validator) are written in each `With*` godoc.

-----

## 📋 Supported Content-Types

For HTTP(S), the response's media type decides the behaviour.

* `text/html`, `application/xhtml+xml` — run through the extraction engine; **only the main text** is returned
* `text/plain`, `text/markdown`, `text/x-markdown`, `text/csv`, `application/json`, `application/xml`,
  `text/xml` — returned as is, without conversion
* `image/*` (any subtype) — returned as raw bytes, without conversion
* Anything else — an unsupported-type error

A `Content-Type` that does not follow the RFC (such as an unclosed `charset="`) is still accepted when the part
before `;` is one of the media types above.

-----

## 🛡️ URL safety (SSRF protection)

HTTP(S) gets two layers of defence.

1. **Before fetching** — `securenet.ValidateURL` resolves the name and rejects private / loopback / link-local destinations.
2. **Right before connecting** — the default HTTP client validates the destination IP again, which also stops DNS rebinding
   (protecting the client itself is [go-http-kit](https://github.com/shouni/go-http-kit)'s job).

`gs://` / `s3://` do not get this check, because the cloud SDK decides where to connect.

-----

## 🤝 Dependencies

* [`github.com/PuerkitoBio/goquery`](https://github.com/PuerkitoBio/goquery) — DOM traversal in `extract`
* [`github.com/andybalholm/cascadia`](https://github.com/andybalholm/cascadia) — precompiled CSS selectors
  (goquery's internal engine)
* [`github.com/shouni/go-http-kit`](https://github.com/shouni/go-http-kit) — the default HTTP client,
  response handling (size cap) and fetch retries
* [`github.com/shouni/go-remote-io`](https://github.com/shouni/go-remote-io) — the GCS/S3 I/O abstraction;
  scheme parsing follows it as well
* [`github.com/shouni/netarmor`](https://github.com/shouni/netarmor) — URL and destination-IP safety checks (SSRF protection)
* [`golang.org/x/net`](https://pkg.go.dev/golang.org/x/net) — `html` (text-node traversal) and
  `html/charset` (charset detection)

-----

## 📜 License

This project is released under the [MIT License](https://opensource.org/licenses/MIT).
