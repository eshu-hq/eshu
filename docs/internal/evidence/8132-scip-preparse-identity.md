# Pre-Parse Identity Folds the SCIP Indexer Content Digest (#8132)

With SCIP indexing on, the pre-parse identity hashed each indexer binary's
resolved path, size, and modification time. A rebuilt image carries the same
indexer version with a new modification time (and sometimes a new size), so
every stored identity mismatched once and each forced reconcile re-parsed the
whole fleet: one full-corpus parse per release while `SCIP_INDEXER` is set.

The identity now folds the hex SHA-256 of each resolved indexer binary's
content. Path, size, and modification time are dropped. A changed digest (a new
indexer version) or a missing/unreadable binary still changes the identity and
forces a re-parse. `IdentityPrefix` moves `preparse:v1:` to `preparse:v2:`, so
the one post-deploy sweep re-parses and re-stamps, and later sweeps skip again.

## Contract

- Equal indexer content under a different path and modification time builds an
  equal identity (`TestPreparseConfigForIgnoresBinaryFilesystemMetadata` uses
  real files with distinct mtimes; `TestIdentityIgnoresBinaryRelocation` pins
  the folding).
- Different indexer content builds a different identity (same tests,
  second half).
- `Missing` stays an input: an unresolvable or unreadable binary changes the
  identity (`TestPreparseConfigForFoldsEachInput` cells `scip binary missing`
  and `scip binary unreadable`, and the `scip binary missing` cell of
  `TestIdentityChangesWithEveryFoldedInput`).

## Performance Evidence:

Baseline: `PreparseConfigFor` ran `os.Stat` per indexer binary (microseconds
per call) but stored an identity no rebuilt image could reproduce, so a
SCIP-enabled deployment paid one full-corpus parse per repository per image
roll (about 24 s wall and 43 s CPU for a 2,011-file repository per
`docs/public/reference/reconciliation-sweep.md`).

After: one `PreparseConfigFor` call reads and SHA-256-hashes each resolved
binary. Measured with a throwaway test on go1.26.9 linux/arm64, 8 SCIP
languages resolving to 6 distinct binaries of random content: 6 x 10 MiB
(60 MiB total) in 60 ms per call, 6 x 50 MiB (300 MiB total) in 300 ms per
call, about 1 ms per MiB. Input shape matches the default `SCIP_LANGUAGES`
allowlist (python, typescript, javascript, go, rust, java, cpp, c).

The call runs once per selection cycle (`preparseConfig` in
`go/internal/collector/repo/git/selection_native.go` builds one shared
`Config` for the batch), next to a cycle of git fetches, checkouts,
discovery, and parses that runs for minutes. The added sub-second per-cycle
cost replaces the per-release full-fleet re-parse above, so the change is
safe. No query, queue, graph-write, or worker path changes.

No-Observability-Change: no new metric, span, or log key. The decision still
emits `eshu_dp_collector_reconcile_parse_skip_decisions_total{outcome}` with
the same outcome labels; a genuine indexer upgrade still logs WARN
`git_reconcile_parse_skip_refused` with `outcome=identity_mismatch` once, then
re-stamps and skips again.
