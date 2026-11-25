// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command tagged-vet-cache wraps `go tool vet` for `go vet -vettool=...` and
// caches `.vetx` fact outputs across distinct `-tags` sweeps in
// `scripts/verify-tagged-builds.sh`.
//
// # Why this exists
//
// Go's toolchain hashes the full `-tags` string into `vetActionID` for every
// package in the transitive build graph. Sweeping dozens of distinct build-tag
// configurations therefore re-runs `cmd/vet` on thousands of unchanged
// transitive dependencies on every tag set, even when their compiled inputs
// and facts did not change.
//
// # Contract
//
//   - Only `VetxOnly == true` invocations (transitive dependency fact
//     generation) are eligible for caching. Target packages (`VetxOnly ==
//     false`), non-`.cfg` invocations (`-V=full`, `-flags`), and configs with
//     `FixArchive` set always pass through to the real vet binary. When
//     `cfg.Stdout` is set on a `VetxOnly == true` cache hit (standard in Go
//     1.27+), an empty file is created at `cfg.Stdout` to match `unitchecker`.
//   - Cache keys are SHA-256 digests of the key version, real vet binary
//     identity (path, size, mtime), `GOOS`/`GOARCH`, vet CLI flags before the
//     `.cfg` argument (excluding `-tags`/`--tags`, whose actual effect is
//     captured by `GoFiles`, `NonGoFiles`, `IgnoredFiles`, and compiled
//     artifacts), and all `unitchecker.Config` inputs (`ID`, `Compiler`,
//     `ImportPath`, `GoVersion`, module metadata, `SucceedOnTypecheckFailure`,
//     `GoFiles`, `NonGoFiles`, `IgnoredFiles`, `ImportMap`, `Standard`,
//     `PackageFile`, and `PackageVetx`).
//   - Files under `GOCACHE` are keyed by their immutable relative path inside
//     the cache root (`gocache:<relpath>`). Ephemeral `$WORK` artifacts
//     (`PackageFile`, `PackageVetx`, and `$WORK`-generated `GoFiles` /
//     `NonGoFiles` such as cgo output) are keyed by SHA-256 content hash and
//     size rather than temp directory path, so identical builds hit across runs
//     and tag-dependent changes invalidate automatically.
//   - Cache writes are atomic (`os.CreateTemp` + `os.Rename`) and best-effort.
//     Any cache read or key-derivation failure falls back to running the real
//     vet binary and propagating its stdout, stderr, and exit code.
//
// # Environment variables
//
//   - `ESHU_TAGGED_VET_REAL_TOOL` (required): path to the real `go tool vet`
//     binary. If unset or empty, the command writes an error to stderr and
//     exits 2.
//   - `ESHU_TAGGED_VET_CACHE_DIR` (optional): directory storing cached `.vetx`
//     files. If unset or empty, all invocations pass through to the real vet
//     binary.
//   - `ESHU_TAGGED_VET_GOCACHE` (optional): Go build cache root; falls back to
//     `GOCACHE`.
package main
