# AGENTS.md — cmd/tagged-vet-cache guidance for LLM assistants

## Read first

1. `go/cmd/tagged-vet-cache/doc.go` — caching contract and env vars.
2. `go/cmd/tagged-vet-cache/main.go` — cache key derivation and pass-through logic.
3. `scripts/verify-tagged-builds.sh` — sole caller that builds and invokes this binary.

## Invariants

- **Stdlib-only.** Do not add `go/internal` or third-party imports; this tool
  is compiled at the start of `scripts/verify-tagged-builds.sh` and must build
  fast with minimal dependencies.
- **Target packages never hit the cache.** Whenever `VetxOnly` is false,
  `VetxOutput` is empty, `FixArchive` is non-empty, or the final argument is
  not a `.cfg` file, execute `ESHU_TAGGED_VET_REAL_TOOL` directly and preserve
  its stdout, stderr, and exit code. On a `VetxOnly == true` cache hit, create
  an empty `cfg.Stdout` file when `cfg.Stdout` is non-empty (Go 1.27+ always
  sets `vcfg.Stdout = a.Objdir + "vet.stdout"`).
- **Exclude `-tags` from cache-key CLI flags.** `cmd/go` forwards `-tags` to
  `cmd/vet` on every invocation, which is why Go's built-in `vetActionID`
  misses across tag sweeps. Strip `-tags`/`--tags` from the hashed flag args;
  any real tag effect already changes `GoFiles`, `NonGoFiles`, `IgnoredFiles`,
  `PackageFile`, or `PackageVetx`.
- **Content-addressed `$WORK` inputs.** Never hash ephemeral `$WORK` directory
  paths for `PackageFile`, `PackageVetx`, or `$WORK`-generated source files
  (such as cgo outputs). Hash file contents + size (and basename for generated
  source files) so identical builds share cache entries while any tag-induced
  change invalidates downstream `.vetx` facts.
- **Fail open to real vet, never to green.** Any error reading `.cfg`, hashing
  an input, or copying a cached `.vetx` must fall back to running the real vet
  binary — never exit 0 without producing valid output.
- **Concurrency-safe writes.** `go vet` spawns one wrapper per package across
  `GOMAXPROCS` workers. Always publish cache entries via `os.CreateTemp` +
  `os.Rename` in the same directory.

## Verification

```bash
cd go && go test -race -count=1 ./cmd/tagged-vet-cache/...
bash scripts/test-verify-tagged-builds.sh
```
