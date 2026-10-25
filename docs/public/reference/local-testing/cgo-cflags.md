# Cgo C Dialect Flag

Eshu compiles tree-sitter grammars through cgo. One of them,
`github.com/alexaandru/go-sitter-forest/perl` (v1.9.9, pinned in `go/go.mod`),
does not compile as C23. Every build that compiles Go sets
`CGO_CFLAGS="-O2 -g -std=gnu17"` so the grammar builds on current toolchains.
Tracking issue: [#9066](https://github.com/eshu-hq/eshu/issues/9066).

## What fails without it

The perl grammar's `scanner.c` includes `<stdlib.h>` and then its own
`bsearch.h`, which defines `void *bsearch(...)`. On glibc 2.43 (Ubuntu 26.04,
the `ubuntu-latest` image from 20261012.163.1) built with GCC 15, whose default
dialect is C23, `<stdlib.h>` defines `bsearch` as a `_Generic` macro. The macro
rewrites that definition into invalid C:

```text
bsearch.h:72:7: error: expected identifier or '(' before '_Generic'
```

The error appears only when the Go build cache misses for that package, so a PR
with a warm cache passes and a PR with a cold cache fails every job that builds
Go. Compiling the cgo packages as `gnu17` skips the macro. The perl grammar has
no newer release than v1.9.9, so there is no upstream fix to take.

## What the flag covers

- **Workflows.** Each `.github/workflows/*.yml` that provisions Go sets
  `CGO_CFLAGS` in its workflow-level `env`. GitHub does not pass `env` between
  workflow files, so every file carries its own.
- **Make and the entrypoints.** `make` targets and the scripts a developer
  starts directly (`scripts/dev/pre-push.sh`, `pre-pr.sh`, `prove.sh`,
  `run-selected-gates.sh`, `scripts/install-local-binaries.sh`, and the
  capability-surface arm of `precommit-go.sh`) export the flag, and so do the
  golden-corpus and Ifa build helpers. Scripts those entrypoints start inherit
  it.
- **Not covered.** Most other scripts that run `go build`, `go test`, or
  `go vet` (for example `scripts/verify-tagged-builds.sh`,
  `scripts/run-go-benchmarks.sh`, and `scripts/generate-*.sh`) do not export
  the flag themselves. Neither does a plain `cd go && go test ./...`. They get
  it from a workflow, from `make`, or from your environment.
- **macOS and clang.** The `build.yml` and `macos.yml` jobs also receive the
  flag. Clang lists `gnu17` among its C modes and defaults to `gnu17` when no
  `-std` is given ([Clang User's Manual](https://clang.llvm.org/docs/UsersManual.html)),
  so the flag leaves a clang compile unchanged. No macOS runner was available
  to prove this locally; the hosted macOS legs are the proof.
- **Not touched.** The `Dockerfile` builds in the `golang:1.26.10-alpine`
  builder stage (musl libc) with `xx-go`, which sets `CC=clang`, and
  `scripts/ci/tools.Dockerfile` is Ubuntu 24.04 (glibc 2.39, GCC 13). Neither
  combination reproduces the failure.

## One-time setting for your host

On a host with glibc 2.43 or newer and GCC 15, set the flag once so every
direct `go` invocation works, including the scripts and commands listed under
"Not covered":

```bash
go env -w CGO_CFLAGS="-O2 -g -std=gnu17"
```

Go's own default for `CGO_CFLAGS` is `-O2 -g`. Setting the variable replaces
that default, so the value repeats `-O2 -g` to keep grammars optimized.

The `make` targets and entrypoints build their value in this order: the
`CGO_CFLAGS` already in the process environment, then `go env CGO_CFLAGS` (so
your `go env -w` value carries through, `-march` flags included), then
`-O2 -g`. They append `-std=gnu17` unless the value already has a `-std=`. The
result goes into the process environment, which takes precedence over the Go
env file for the child `go` processes.

A host that already sets `CGO_CFLAGS` through `go env -w` hides the failure
from local reproductions. To reproduce it, run with `GOENV=/dev/null` and a
throwaway `GOCACHE`.

## CI build time

`CGO_CFLAGS` is part of Go's action IDs for cgo packages, so changing it
rebuilds them. The Go build caches in CI key on the `go.sum` hash, and an
exact-key cache hit skips the save, so the first runs recompile the cgo
packages until `go.sum` changes. A cold local build of the 22 third-party cgo
packages took 17 s. The hosted-runner cost was not measured.

## The guard

`scripts/verify-cgo-cflags-guard.sh` is the `cgo-cflags-guard` gate. It fails
when:

- a workflow that compiles Go has no workflow-level `CGO_CFLAGS` with
  `-std=gnu17`, or any `CGO_CFLAGS` assignment drops the flag;
- a script under `scripts/` that runs `CGO_ENABLED=1 go`, or a developer
  entrypoint, lacks a non-comment line that sources `scripts/lib/cgo-cflags.sh`
  or a non-comment call to `eshu_export_cgo_cflags`. A comment that names the
  library does not count.

The guard covers exactly those files. It does not detect a script that runs
`go` without the `CGO_ENABLED=1` prefix.

Its mirror test, `scripts/test-verify-cgo-cflags-guard.sh`, plants each
violation in a fixture tree, including a comment-only mention and a source with
no export, and compiles a probe that defines `bsearch` after `<stdlib.h>`: it
fails with the default dialect on an affected host and passes with the chosen
flag.

## Removing the flag

Remove it when `go/go.mod` pins a `go-sitter-forest/perl` release that builds
under C23. Delete the `CGO_CFLAGS` lines from the workflows and Makefile,
delete `scripts/lib/cgo-cflags.sh` and its source lines (and the library's
trigger entries in `specs/ci-gates.d/` and `specs/ci-pr-workflows.d/`), retire the
`cgo-cflags-guard` gate, and prove a cold-cache build on the current runner
image without the flag.
