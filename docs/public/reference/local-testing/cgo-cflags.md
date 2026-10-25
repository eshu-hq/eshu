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

## Where the flag lives

- **Workflows.** Each `.github/workflows/*.yml` that provisions Go sets
  `CGO_CFLAGS` in its workflow-level `env`. GitHub does not pass `env` between
  workflow files, so every file carries its own.
- **Scripts.** `scripts/lib/cgo-cflags.sh` exports the value. It appends to a
  `CGO_CFLAGS` the caller already set, keeps a caller's own `-std=`, and is
  idempotent. The developer entrypoints (`make`, `scripts/dev/pre-push.sh`,
  `pre-pr.sh`, `prove.sh`, `precommit-go.sh`, `run-selected-gates.sh`,
  `scripts/install-local-binaries.sh`) and the scripts that run
  `CGO_ENABLED=1 go build` source it.
- **Makefile.** It exports the same value for `make` targets.
- **macOS and clang.** The `build.yml` and `macos.yml` jobs also receive the
  flag. Clang lists `gnu17` among its C modes and defaults to `gnu17` when no
  `-std` is given ([Clang User's Manual](https://clang.llvm.org/docs/UsersManual.html)),
  so the flag leaves a clang compile unchanged. No macOS runner was available
  to prove this locally; the hosted macOS legs are the proof.
- **Not touched.** The `Dockerfile` image builds use `xx-go` with clang on
  Alpine 3.21 (musl libc), and `scripts/ci/tools.Dockerfile` is Ubuntu 24.04
  (glibc 2.39, GCC 13). Neither combination reproduces the failure.

Go's own default for `CGO_CFLAGS` is `-O2 -g`. Setting the variable replaces
that default, so the flag value repeats `-O2 -g` to keep grammars optimized.

A host that sets `CGO_CFLAGS` through `go env -w` hides the failure from local
runs. Reproduce with `GOENV=/dev/null` and a throwaway `GOCACHE`.

## The guard

`scripts/verify-cgo-cflags-guard.sh` is the `cgo-cflags-guard` gate. It fails
when a workflow that compiles Go has no workflow-level `CGO_CFLAGS` with
`-std=gnu17`, when any `CGO_CFLAGS` assignment drops the flag, or when a script
that runs `CGO_ENABLED=1 go` or a developer entrypoint does not source the
library. Its mirror test, `scripts/test-verify-cgo-cflags-guard.sh`, plants each
violation in a fixture tree and compiles a probe that defines `bsearch` after
`<stdlib.h>`: it fails with the default dialect on an affected host and passes
with the chosen flag.

## Removing the flag

Remove it when `go/go.mod` pins a `go-sitter-forest/perl` release that builds
under C23. Delete the `CGO_CFLAGS` lines from the workflows and Makefile,
delete `scripts/lib/cgo-cflags.sh` and its source lines, retire the
`cgo-cflags-guard` gate, and prove a cold-cache build on the current runner
image without the flag.
