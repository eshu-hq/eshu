# Cgo C Dialect Flag

Eshu compiles tree-sitter grammars through cgo. One of them,
`github.com/alexaandru/go-sitter-forest/perl` (v1.9.9, pinned in `go/go.mod`),
does not compile as C23. Every workflow that provisions Go, `make`, and the
developer entrypoints set `CGO_CFLAGS="-O2 -g -std=gnu17"` so the grammar builds
on current toolchains. Other direct `go` runs need the flag in your environment;
see [What the flag covers](#what-the-flag-covers) and
[One-time setting for your host](#one-time-setting-for-your-host).
Tracking issue: [#9066](https://github.com/eshu-hq/eshu/issues/9066).

## What fails without it

The perl grammar's `scanner.c` includes `<stdlib.h>` and then its own
`bsearch.h`, which defines `void *bsearch(...)`. On glibc 2.43 (Ubuntu 26.04)
built with GCC 15, whose default dialect is C23, `<stdlib.h>` defines `bsearch`
as a `_Generic` macro. The macro rewrites that definition into invalid C:

```text
bsearch.h:72:7: error: expected identifier or '(' before '_Generic'
```

The error appears only when the Go build cache misses for that package, so on
such a host a warm cache passes and a cold cache fails every job that builds Go.
The workflows run on `ubuntu-24.04` (GCC 13, glibc 2.39), which does not hit it,
so the flag is defensive there and guards a local or CI host that moves to
GCC 15 with glibc 2.43 (it was the 26.04 `ubuntu-latest` image before the
runner pin). Compiling the cgo packages as `gnu17` skips the macro. The perl grammar has
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
  `-std=gnu17`, or any `CGO_CFLAGS` assignment drops the flag, adds a second
  `-std=`, uses a `${{ }}` expression, or unsets the variable. A whole-line or
  trailing `#` comment that names `-std=gnu17` does not count as the flag;
- a script under `scripts/` that runs `CGO_ENABLED=1 go`, or a developer
  entrypoint, lacks a non-comment line that sources `scripts/lib/cgo-cflags.sh`
  or a non-comment call to `eshu_export_cgo_cflags`. A comment that names the
  library does not count.

The guard covers exactly those files. It does not detect a script that runs
`go` without the `CGO_ENABLED=1` prefix.

It is a line-based text check, not a shell parser. These do not satisfy it: a
whole-line comment, a trailing `#` comment, an `echo` string that holds none of
`;`, `&`, `|`, `(`, `{`, `then`, `do`, or `else`, and a definition written
`name() {`, `name () {`, or `function name`. Known and accepted, because each
needs a parser: a string that holds one of those words or separators, a heredoc
body line, and a call inside a function or branch that never runs. The build helpers call the export at the top of the
function that runs `go`, and the mirror tests run the real entrypoints.

The workflow check also accepts, for the same reason: a later `-std=` on the
same command line after the assignment (`CGO_CFLAGS=-O2 go build -x -std=gnu17`),
`env -i` or another way to clear the variable, and a flag set through a YAML
anchor, a job-level `env`, or `go env -w`; the last group fails safe because no
workflow-level flag is found. A quoted value that holds ` #`, `;`, `&`, or `|` is
cut short and fails safe.

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
`cgo-cflags-guard` gate, and prove a cold-cache build without the flag on a
host with glibc 2.43 and GCC 15 (the 24.04 runners pass either way, so they
prove nothing).
