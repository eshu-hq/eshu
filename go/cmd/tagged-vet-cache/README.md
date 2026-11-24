# cmd/tagged-vet-cache

## Purpose

`tagged-vet-cache` is a stdlib-only `-vettool` wrapper invoked by
`scripts/verify-tagged-builds.sh`. It caches `VetxOnly == true` transitive
dependency `.vetx` fact files across distinct `-tags` invocations of `go vet`
so unchanged dependencies are not re-analyzed on every tag set.

## Ownership boundary

Owns only `.vetx` caching for `VetxOnly == true` invocations of `go tool vet`.
Does not discover build tags, expand build constraints, or schedule packages
— that belongs to `scripts/verify-tagged-builds.sh`. Target packages
(`VetxOnly == false`) always execute the real `go tool vet` binary directly.

## Exported surface

Executable `main` package with no exported library API. Configured via
environment variables:

- `ESHU_TAGGED_VET_REAL_TOOL` — path to `go tool vet` (required; exits 2 if empty)
- `ESHU_TAGGED_VET_CACHE_DIR` — ephemeral directory for `.vetx` cache entries
  (if empty, passes every invocation through)
- `ESHU_TAGGED_VET_GOCACHE` — Go build cache root (falls back to `GOCACHE`)

See `doc.go` for the full caching and invalidation contract.

## Dependencies

Go standard library only. Zero external or `go/internal` imports so the
wrapper compiles quickly before the tagged-builds sweep starts.

## Telemetry

None. This is a short-lived local and CI build-tool wrapper invoked directly
by `go vet -vettool=...`; diagnostics come straight from the underlying
`go tool vet` stderr and exit status.

## Gotchas / invariants

- Never cache `VetxOnly == false` invocations: target packages must always run
  real `go tool vet` so analyzer diagnostics and exit codes are preserved.
- Never include ephemeral `$WORK` paths in the cache key for `PackageFile`,
  `PackageVetx`, or `$WORK`-generated `GoFiles`/`NonGoFiles`; hash their file
  contents instead so identical artifacts hit across runs while tag-induced
  changes invalidate.
- Cache writes use `os.CreateTemp` + `os.Rename` in the cache directory so
  parallel `go vet` workers cannot observe partial `.vetx` files.
- If a cached `.vetx` copy fails midway, remove the partial `VetxOutput` file
  and fall through to running real `go tool vet`.

## Related docs

- `scripts/verify-tagged-builds.sh`
- `scripts/test-verify-tagged-builds.sh`
- `docs/public/reference/local-testing/proof-notes.md`
