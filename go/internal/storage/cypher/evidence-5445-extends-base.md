# Evidence — #5445 Kustomize EXTENDS_BASE edge (hot-path storage/cypher)

The Kustomize overlay→base `EXTENDS_BASE` edge writer
(`canonical_kustomize_edges.go`) and its supporting repo-scoped index
(`kustomize_overlay_repo_id`, `schema_tables_indexes.go`) touch the canonical
graph-write hot path. This note records the required performance and
observability evidence.

Benchmark Evidence: the `kustomize_overlay_repo_id` repo-scoped index seek is
280.7us -> 241.7us warm median (p90 385.3us -> 311.5us) at 100k KustomizeOverlay
nodes across 5,000 repos; measured wall-clock (pinned NornicDB Bolt returns no
PROFILE), full table below.

No-Regression Evidence: the edge write adds only one repo-scoped index seek plus
one bounded (single-digit-per-repo) graph write; the parser already computes
`bases` (zero new parse work), and the MATCH/MATCH/MERGE + generation-gated
Drain-marked retract shape is identical to the accepted GitLab NEEDS precedent,
scoped to one repo (no all-graph scan). Details below.

No-Observability-Change: the EXTENDS_BASE writer emits no new metric; it rides
the existing CanonicalNodeWriter phase-span/recordAtomicWrite telemetry like its
sibling structural-edge writers, with slog.WarnContext on resolver failure.
Details below.

## Performance Evidence

The resolver read is a repo-scoped index seek, not a label scan:
`MATCH (ko:KustomizeOverlay) WHERE ko.repo_id = $repo_id RETURN ko.uid, ko.path,
ko.base_refs`, anchored on the new `kustomize_overlay_repo_id` index.

## Benchmark Evidence

Isolated NornicDB instance, 100,000 `KustomizeOverlay` nodes across 5,000 repos
(20/repo), reading one target repo's 20 rows, warm:

| Metric | Before index (`ONLINE` absent) | After index |
| ------ | ------ | ----- |
| median | 280.7us | 241.7us |
| p90    | 385.3us | 311.5us |

The pinned NornicDB Bolt transport returns no `PROFILE`/`EXPLAIN` plan tree
(documented in `docs/internal/evidence/5410-sql-relationships-performance.md`),
so wall-clock on the discriminating shape is the recorded proof, per that note's
own fallback.

## No-Regression Evidence

The edge write adds bounded, low-single-digit work per repo: the parser already
computes `bases` unconditionally (`kustomize_semantics.go`), so parsing does zero
new work; the only added cost is one repo-scoped index seek plus one graph write
per resolved edge, and real-world overlay counts are single-digit per repo. The
`MATCH…MATCH…MERGE` edge shape and the generation-gated, Drain-marked
retract-before-merge are identical to the already-accepted GitLab `NEEDS`
(`canonical_gitlab_edges.go`) precedent; no new all-graph scan is introduced (the
retract is scoped to one repo's overlay set).

## No-Observability-Change

The `EXTENDS_BASE` writer emits no new metric. It rides the existing
`CanonicalNodeWriter` phase-span and `recordAtomicWrite` telemetry that covers
every structural-edge statement generically, consistent with its sibling
structural-edge writers (GitLab, Atlantis). Resolver failures are surfaced via
`slog.WarnContext` with `repo_id`/`generation_id`, matching the tfstate resolver
precedent. No dashboard, metric, or span contract changes.
