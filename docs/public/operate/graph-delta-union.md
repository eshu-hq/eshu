# Delta-base union (#7797)

Delta-active scopes project delta-only: `LoadFacts` read one
(scope, generation), so every from-empty projection of a delta (bootstrap,
refinalize, rebuild) restored only the delta's files. The delta-base union
fixes the read: a delta generation with a projected base loads overlaid on
that base, the delta winning per stable key with touched paths dropped, so
writes carry the full set while retracts stay file-scoped.

## Base rule

For a delta generation with baseline commit B, the base is the newest
projected full generation in the scope at commit B: `is_delta = false`,
`activated_at IS NOT NULL`, status active/completed/superseded, intact
statuses first. A superseded base with any retention prune event refuses, as
does a base row with no facts (a stored full always carries at least its
repository fact) and a delta with no projected base at all.

A refused delta fails closed through the fence as `refused_base_missing`
(`projector_delta_base_missing` failure class): the generation is marked
superseded, nothing is projected, and a per-repository reindex watermark
routes the next selection to the forced-full path, which restores the base.
The watermark write retries on the next sync's refusal, so a transient
failure self-heals.

## Operator signals

- `eshu_dp_projector_delta_union_total` by closed `outcome`
  (`union_base_found`, `union_base_missing_refused`, `union_resolve_failed`,
  `union_scope_mismatch`). A non-refusal lookup error counts
  `union_resolve_failed` and retries through the generic fail path; a
  resolved union whose delta carries no `delta_generation` fact loads
  additively with a WARN and counts `union_scope_mismatch`.
- `eshu_dp_projector_delta_union_facts` histogram of base/delta/union fact
  counts by `outcome` and `kind`. Counts are values, never labels; commit
  SHAs, scope ids, and generation ids never become labels.
- One `projector.delta_union` span event and one INFO log per union decision.
- `GET /api/v0/admin/delta-union/completeness` returns the read-only
  completeness report: per repository, the graph's file set against the
  ACTIVE set (base-full overlaid with the active delta), with per-path
  entity counts, outcome `complete`, `mismatch`, or `skipped`, and the
  base/active generations behind each verdict. Repeat the `scope` query
  parameter to name scopes; at least one scope is required, and an empty
  filter is rejected with 400 instead of fanning out over every active
  generation. Past the repository cap the call fails loudly instead of
  paging silently, and a filter wider than the cap fails before the ACTIVE
  fact fan-out. A graph outage answers the retryable `503` with `Retry-After`
  and a graph deadline answers `504`, both with fixed verdict text (#8292).
  Any other failed ACTIVE or graph read answers `500` with the fixed
  detail `delta-union completeness check failed`, or `499` when the client
  canceled; the body never carries the backend error (#7674). Per-path counts cover Function entities only, matching the
  graph readers. A mismatch names the diff for a forced full re-parse;
  the check never repairs. Empty delta-active repositories pass, scopes
  without an active generation skip with a reason, scopes whose active
  generation is not a delta skip without a graph read, and
  tombstoned-only rows are excluded on both sides.

## Performance Evidence

Base lookup on PostgreSQL 16, 60,000 `scope_generations` rows (40 scopes x
1,500 generations, mixed full/delta and five statuses), `EXPLAIN (ANALYZE,
BUFFERS)`: 0.328 ms and 56 buffers with 898 rows removed by filter on the
pre-existing status bitmap, which serves the lookup with no dedicated
index. A partial index over projected full generations was evaluated
(one seek, 0.132 ms, 19 buffers, byte-identical row) and dropped: it
deterministically flipped the retention prescreen plan to sequential
scans, failing that lane's blocking gate, while its own win was small
and mainly growth protection. The blocking gate plus accuracy outrank
an unproven growth-protection index. A plain
`(scope_id, is_delta, source_commit_sha)` index is never chosen by the
planner and was not shipped. The lookup runs once per delta fact load and
once per delta fact count. Per-load cost is one target-row read, one base
bitmap probe, one prune-event probe only for superseded bases, plus the
base fact pages the projection would have skipped before: a delta
projection now loads base plus delta facts, and `CountFacts` reports that
upper bound so the large-generation semaphore classifies on the union.

## Observability Evidence

Union decisions emit the counter, the histogram, the span event, and the log
line above; refusals add the fence counter outcome, the WARN log, and the
watermark write (WARN on watermark failure). The golden-corpus `delta-union`
phase and the admin check compare GRAPH against ACTIVE on every run.

## Repair-load measurement plan

Measurement itself is owner-authorized; this note ships the plan and the
decision rule. Per delta-active scope under repair, record: base fact count,
delta fact count, union fact count, base-seek time (`EXPLAIN (ANALYZE,
BUFFERS)` on the scope), load stage duration, projection stage duration,
large-generation semaphore holds and wait time, and the union outcome counts
from the counter.

Decide against the last known-good full timings for the same scope: the
repair load should land within the full-projection band for the union fact
count (same rows, file-scoped retracts instead of full retracts). A scope
whose repair load exceeds its full band by more than the base-seek plus
overlay cost regresses the repair path: hold the scope on full generations
and file an issue with the per-scope table. The 161-scope production repair
stays an owner act; this change only makes each scope's delta projectable.

## Dialect status

The union is Postgres-side; no new Cypher was added for it. The
completeness check's per-repo Cypher anchors on `Repository {id}` with a file
fan-out plus an optional Function containment count, the same vocabulary the
deadcode surface runs in production. Validated 2026-10-15 against the pinned
NornicDB image (`nornicdb-amd64-cpu:fix-500-e022384c`, digest `sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`;
own container on moved ports, removed after): the exact statement returns
evaluated `rel` strings and `entities` int64s, with `a.go -> 2` and an
uncontained `b.go -> 0`, so the single-OPTIONAL-MATCH-plus-count shape is
safe on the pinned build (the #6262 hazard needed a second chained OPTIONAL
MATCH or call-expression projections; neither appears here). Neo4j runs the
identical statement unchanged.

Depends on #7802 (retention guard) staying open: retention may still prune a
union base, which this change answers with a refusal plus forced full, not
with retention restraint.
