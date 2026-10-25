# Epoch

## Purpose

This package owns the git collector's content epoch ledger (#9097). Every git
freshness hint folds one content version string. The ingestion store treats an
equal hint and commit as a duplicate and stores nothing (#7978), so the string
must move whenever the collector or a parser emits different facts for
unchanged source (ruling a9-117 d6). It used to be the hand-bumped constant
`git-collector.vN`: every bump conflicted with every other open branch, and two
branches could bump to the same `vN` for different output and both merge
cleanly (`428145cee7`). The version is now `git-collector.<epoch>`, where the
epoch comes from a committed ledger that nobody edits by hand.

## Ownership boundary

Owns the ledger format, the epoch advance rule, and the embedded ledger read.
It does not compute buckets: the gate test in package `git`
(`snapshot_content_version_gate_test.go`, `content_version_buckets_test.go`)
runs the production snapshotter over `../testdata/content_version`, attributes
each fact to a fixture file, and calls `Repin` or `Drift`. Folding the epoch
into the hint stays in `../snapshot_freshness.go`.

## Exported surface

- `ledger.tsv` — the committed ledger, embedded in the binary. One `epoch`
  line (`epoch<TAB><16 hex><TAB><seal>`) and one line per bucket:
  `bucket<TAB>input_digest<TAB>fact_count<TAB>output_digest`, sorted by name.
  A bucket is a fixture file, or `_repository` for facts that name no single
  file (the repository fact, follow-ups, the documentation source).
- `Current` — the epoch of the embedded ledger, parsed once per process.
- `Bucket`, `Ledger`, `RepositoryBucket`, `LedgerFile` — the ledger model.
- `Parse`, `Ledger.Marshal`, `Ledger.Sealed`, `SealOf` — read, write, and seal.
- `Repin` — the next ledger for a computed bucket set.
- `Drift` — every difference between the pinned ledger and a computed set.
- `ValidEpoch` — the 16-lowercase-hex epoch format.

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`crypto/sha256`, `embed`, `sync`). Package `git`
imports this package; it never imports `git`.

## Telemetry

None. The package has no runtime state beyond the parsed epoch. The folded
version is part of the freshness hint, as the old constant was; no metric,
span, or log key names it.

## Gotchas / invariants

- Re-pin, never hand-edit: `cd go && go test ./internal/collector/repo/git
  -run TestGenerationContentVersionPinsCollectorOutput
  -update-content-version-pin`.
- The epoch advances only when a bucket kept its input digest and changed its
  output digest. A new, removed, or edited fixture file keeps the epoch, so a
  fixture-only change stores nothing new in deployed repositories.
- Add `-advance-content-epoch` when one change edits a fixture file and its
  output together, or adds a parser path rule outside the registry
  fingerprint. The ledger reads both as fixture-only changes.
- The advanced epoch is the first 16 hex characters of
  `sha256(parent + "\n" + sorted output digests joined by "\n")`. Two branches
  that advance from one parent with different outputs land on different epochs
  and conflict on the `epoch` line. Identical output moves share an epoch,
  because the facts are the same.
- On a ledger conflict, take `origin/main`'s copy and re-pin. The re-pin sees
  the branch's output as moved against main's and lands on a third epoch. A
  ledger whose bucket lines were combined by hand fails the seal check in the
  gate and in `Repin`.
- An epoch advance changes every git hint once, so every repository stores one
  new generation on its next sync. `Current` parses a small file once per
  process; no hot path changes.

No-Regression Evidence (#9097): the per-snapshot path is unchanged in work.
`GenerationContentVersion` stays a `sync.OnceValue` read, and
`snapshotFreshnessHint` hashes one version string that is 10 bytes longer
(`git-collector.<16 hex>` instead of `git-collector.v15`). A throwaway benchmark
at base `7724e00960` and at this change (local commit `006b0b5b9e`, whose Go
source equals the final head's; later amends touched docs only) (`-cpu 1`, three rounds each, Apple
M5 Max, Go 1.26.10, host load average about 25) measured
`GenerationContentVersion` at 2.07-2.44 ns/op on base and 2.10-5.06 ns/op on
head, 0 allocations on both, and `snapshotFreshnessHint` on a one-file snapshot
at 688-1860 ns/op on base and 784-1878 ns/op on head, 344 B and 9 allocations
on both. The ranges overlap; the spread is host noise. The one new cost is
parsing the embedded 73-line ledger once per process: 7.8-9.1 µs, 24,512 B, 77
allocations. No Postgres, graph, queue, or worker path changes. Input shape:
the content-version fixture ledger (67 buckets) and a one-file test snapshot.

No-Observability-Change: no metric, span, log key, or status field changes. The
epoch reaches operators the same way the old constant did, inside the
freshness hint, and the store's skip and re-project signals are unchanged.

## Related docs

- `docs/public/reference/reconciliation-sweep.md` ("Content-version gate
  coverage")
- `go/internal/collector/repo/git/AGENTS.md` (the "never blank the hint" rule)
