# Collector Diagnostics

This file carries collector-emitted operator diagnostics that are too
detailed for the package README. It currently covers the repository
basename-collision signal (issue #3677).

## Repository basename-collision diagnostic (issue #3677)

`NativeRepositorySelector` emits an observability signal after filesystem-mode
discovery when the same repository basename appears at more than one discovered
path. This is a HEURISTIC for likely accidental corpus nesting (e.g. the 4×
inflation caused by `repos/repos/repos/…` recursive copies), not a true
duplication check: distinct repositories can legitimately share a basename
(`org-a/utils` and `org-b/utils`, or monorepo `common/` directories). The signal
does not change which repos are indexed.

The report fires only on a changed batch: it runs after
`syncFilesystemRepositories`, which returns an empty path set for an unchanged
corpus (fixture-manifest match,
[`git_selection_filesystem.go`](git_selection_filesystem.go) lines 29-32). The
report is gated on a non-empty sync, so it fires on the first run and whenever
the on-disk corpus changes, and stays silent on steady-state re-polls under
`Service.Run` — no per-interval log or metric spam for an unchanged corpus.

Observability Evidence: `eshu_dp_repository_basename_collision_total` is a
plain counter (no path or basename labels; those are unbounded) that advances
by the number of surplus (non-first) colliding paths detected in one cycle. A
non-zero value is a LIKELY signal of accidental corpus nesting and warrants
inspecting the logged paths before concluding duplication. Read the accompanying
structured warning log (`"repository basename collision detected (possible
accidental corpus nesting)"`, with fields `identity`, `path_count`,
`surplus_count`, and `path_sample`) to see which basename collides and a bounded
sample (up to 5) of the offending paths. `surplus_count` (= `path_count` − 1)
reconciles the log with the metric delta. Together, the metric fires an alert
and the log provides the investigation anchor an operator needs to triage the
corpus without DB forensics.

No-Regression Evidence: `reportRepositoryBasenameCollisions` runs O(n) over the
already-discovered `repositoryIDs` slice (a single-pass `map[string][]string`
group, no filesystem I/O, no git exec, no extra directory walk beyond what
`discoverRepoRoots` already performed). It runs only on a changed batch in
filesystem mode (gated on a non-empty sync), adds negligible wall time, and does
not alter selection or indexing behaviour. The changed-batch gate is stateless
(it reads only the sync result), so it is safe under the single `Service.Run`
polling goroutine without added synchronisation.

The report has three correctness properties relevant to issue #3700:

- **Completeness (pre-shard set).** It inspects the full pre-shard
  `selection.RepositoryIDs`, not the post-shard subset passed to the sync
  function. With sharding active, colliding basenames (e.g. `worker` and
  `repos/worker`) may hash into shard buckets that no single shard's post-shard
  subset holds together via FNV32a, so a per-shard check is permanently silent
  even though the corpus is inflated. Using the pre-shard set means the global
  collision is always visible. This applies identically in copy mode and
  `ESHU_FILESYSTEM_DIRECT=true` mode (both share the same call site and gate); the
  diagnostic fires in direct mode on a nested corpus at the unsharded default
  exactly as it does in copy mode.
- **Single-emit (index-0 shard).** Because every shard instance inspects the same
  global pre-shard set, the report runs only on the index-0 shard
  (`RepoShardIndex == 0`). Letting all `N` shards report would inflate one real
  collision into `N` duplicate WARN lines and an `N×` metric reading, breaking any
  alert threshold tuned to the true surplus. Pinning to shard 0 fires the global
  signal exactly once per changed batch. Shard 0 exists for any shard count `>= 1`,
  and at the unsharded default the index is 0, so single-instance behaviour is
  unchanged.
- **Changed-batch anti-spam, decoupled from ownership.** The emit gate keys on
  `corpusChanged` — the full-corpus changed signal returned by
  `syncFilesystemRepositories` (the `FilesystemRoot` fingerprint vs the stored
  manifest) — NOT on `len(repoPaths)`. `repoPaths` is the index-0 shard's *own*
  materialized subset, which is empty whenever every colliding repo hashes to a
  non-zero shard. Gating the emitter on its own subset would silence the
  diagnostic exactly in the inflated-corpus case it targets (issue #3700 P2, Codex
  review on PR #3706). The fingerprint covers the whole root, so `corpusChanged`
  is identical on every shard: the designated emitter fires regardless of which
  repos it owns, and stays silent on an unchanged re-poll (`corpusChanged` is
  false). At the unsharded default this is equivalent to the old
  `len(repoPaths) > 0` gate, since shard 0 then owns the full set.

Verified by
`go test ./internal/collector -run 'TestReportRepositoryBasenameCollisions|TestNativeRepositorySelectorFilesystem_BasenameCollision|TestNativeRepositorySelectorFilesystemDirect' -count=1`
(10 tests): the five unit tests (collisions-fire, no-collisions-silent, nil-safe,
empty-silent, counter-matches-surplus-count); the end-to-end
`TestNativeRepositorySelectorFilesystem_BasenameCollisionWarning`;
`TestNativeRepositorySelectorFilesystem_BasenameCollisionOnlyOnChange` (fires on
first run, silent on unchanged re-poll, re-fires on change);
`TestNativeRepositorySelectorFilesystemDirect_NestedCorpusCollisionFires` (direct
mode at the unsharded production default — the #3700 regression);
`TestNativeRepositorySelectorFilesystemDirect_ShardedCollisionSingleEmit` (sharded
direct mode: the global collision fires from shard 0 only, aggregate metric
equals the true surplus rather than `N×`); and
`TestNativeRepositorySelectorFilesystemDirect_CollisionFiresWhenShardZeroEmpty`
(the colliding pair hashes entirely to a non-zero shard so shard 0 owns nothing;
the diagnostic still fires once from shard 0 via `corpusChanged`, and the re-poll
stays silent — the #3700 P2 regression).
