# Delta-base union (#7797)

Delta-active scopes project delta-only: `LoadFacts` read one
(scope, generation), so every from-empty projection of a delta (bootstrap,
refinalize, rebuild) restored only the delta's files. The delta-base union
fixes the read: a delta generation with a projected base loads overlaid on
that base, the delta winning per stable key with touched paths dropped, so
writes carry the full set while retracts stay file-scoped.

## Base rule

For a delta generation, the base is the newest projected full generation in
the scope at its union-base commit,
`COALESCE(delta_union_base_commit_sha, delta_baseline_commit_sha)`:
`is_delta = false`, `activated_at IS NOT NULL`, status
active/completed/superseded, intact statuses first. A delta stored before
#8780 has no union-base column, so it resolves at its baseline, which was then
always its base full's commit. A superseded base with any retention prune event refuses, as
does a base row with no facts (a stored full always carries at least its
repository fact) and a delta with no projected base at all.

A refused delta fails closed through the fence as `refused_base_missing`
(`projector_delta_base_missing` failure class): the generation is marked
superseded, nothing is projected, and a per-repository reindex watermark
routes the next selection to the forced-full path, which restores the base.
The watermark write retries on the next sync's refusal, so a transient
failure self-heals.

## Projection boundary (#8779)

Base rows in a union keep the base generation's id. The fact load that built
the overlay also returns that base id and the union outcome
(`FactStore.LoadProjectionFacts`), and both projection loops, the projector
service and the bootstrap-index projector, set it on the generation they hand
to `Project` as `DeltaUnionBaseGenerationID`. Nothing resolves the base a
second time, so the check always uses the base the facts came from.

- The fact boundary accepts a fact when it belongs to the projected scope and
  either to the delta or to the named base. A fact from any other generation
  or scope still fails the whole projection before any write, and a base id on
  a non-delta generation, or one that names the delta itself, is an error.
- Canonical and content materialization read the full union, stamped with the
  delta generation, with file-scoped retracts.
- Reducer intents, per-fact and scope-generation, derive only from the delta's
  own facts. The git collector withholds the whole-repository follow-ups and
  the value-flow marker on a delta. A base row would fire those domains under
  the delta against handlers that load only the delta's facts and retract the
  whole scope.
- A union whose outcome is `union_scope_mismatch` (the delta carries no
  `delta_generation` repository fact, so the overlay ran additively) is
  refused before any write and fails through the generic path.
- The `build_projection` stage log carries `generation_fact_count`,
  `union_base_fact_count`, and `reducer_intent_count`.

## Cumulative deltas (#8780)

A git delta is cumulative from the scope's newest activated full generation F,
so a delta over an active delta still has a full base. The collector diffs the
changed set from F's commit, deletes `D(F..head) ∪ D(active..head)`, records
the active commit as `delta_baseline_commit_sha` (the #7319 fence commit, which
keeps its meaning), and records F's commit as `delta_union_base_commit_sha`.
The second deleted term matters: a file the active chain added that the head
no longer has is in neither F nor `F..head`, and without it no retract would
name it. A cumulative delta that touches at least half of F's tracked files
takes a full instead (`delta_exceeds_base_half`). Retention keeps the same F
for a delta-active scope (#7802), provided no activated full carries a status
outside active, completed, or superseded. Retention's `delta_base` has no
status filter and the collector's read has one, so if the two ever disagreed
the union read would refuse base-missing and force a full.

A legacy delta whose baseline is another delta's commit and that records no
union base still resolves at that commit, finds no full, and refuses as
`refused_base_missing`. A burst delta diffed from a commit that is no longer
active refuses at preflight (`refused_active_differs`, no watermark); the
next sync re-diffs it as a cumulative delta fenced on the new active commit.
The search-document and `changed-since` windows of a delta-active scope last
longer under cumulative deltas; that gap is #8801. A delta-active scope never
takes the reconcile parse skip (`delta_active`), so a churning repository pays
one real full parse per reconcile interval, which re-roots the chain on the
new full; quiet repositories still skip.

## Successor guard anchor (#8750)

The ingestion commit's destructive-successor guard (#7926) sizes a git delta
by the same union the projector will build, not by the delta's own stream. It
finds the delta's anchor with the base rule above (the newest projected full
generation at the delta's union-base commit, COALESCEd to its baseline,
#8780), through the same resolver, so the guard and the projection agree
about the base. A delta with no anchor stores;
the projector then refuses it as `refused_base_missing` and arms the reindex
watermark, which a store-side refusal would not.

The guard stores a delta unless its union would hold under half of the
anchor's files. It checks a zero-query lower bound first
(`anchorFiles - |touched ∪ live| + live`). Only a delta that may remove most
of the anchor's files reads the anchor's file paths once to get the exact
union. Paths are matched the way the overlay matches them, so a rename nets to
zero and a deleted path that was never indexed removes nothing. Every guarded
commit logs the `successor_guard` stage and counts
`eshu_dp_ingestion_successor_guard_decisions_total` by `kind`, `rule`, and
`outcome`; a refused delta leaves the prior generation active and the next
sync recollects. The sizing evidence is in
`docs/internal/evidence/8750-successor-guard-sizing.md`.

## Reducer manifest dependency read (#9991)

`ListActivePackageManifestDependencyFacts`, the manifest pin read behind
package-source correlation, security-alert reconciliation, and supply-chain
impact, used to join only the active generation, so a delta-active
repository showed only its changed pins. It now overlays the delta on the
same base the projector uses: `resolveDeltaBases` picks the base (the same
batched base lookup and retention probe the projector runs for one scope) and
`facts.OverlayDeltaBase` applies the delta's stable keys, tombstones, and
touched paths. A refused base, a legacy delta with no baseline, or a delta
with no `delta_generation` fact keeps the delta-only rows. The read counts
each decision on
`eshu_dp_package_manifest_dependency_read_delta_union_total` and logs a WARN
line for a refused base or a mismatch. Other `facts_active_*` readers that
join only the active generation still see delta-only rows (#10003; listed in
`docs/internal/evidence/9991-manifest-read-delta-union.md`).

## Golden-corpus delta readers (#10022)

The golden `delta-union` phase compares file sets only, so it cannot catch a
reducer reader that loses unchanged base rows. The opt-in `delta-readers`
phase checks reducer outputs for one delta-active git repository instead.
Every other corpus repository is collected in filesystem mode, which never
produces a delta. Cassette replay can't produce one either, because it never
sets `ScopeGeneration.IsDelta`. So the orchestrator
(`scripts/lib/golden-corpus-delta-reader.sh`) does the following:

1. It stages `tests/fixtures/golden_delta_union/base` as the bare git origin
   `acme/golden-delta-reader`.
2. It collects that origin with `eshu-collector-git` in explicit mode,
   through a private `GIT_CONFIG_GLOBAL` whose `insteadOf` maps
   `https://github.com/` to the origin. This runs after the first drain,
   because a new repository changes the repository catalog. The reducer's
   activation-obligation consumer then refuses partition-scoped maintenance
   (`catalog_changed`), so only the corpus-wide maintenance passes publish the
   repository's backward evidence and activate its full generation.
3. After the maintenance drains, it runs `-delta-reader-stage=full`. Every
   edge in `graph.delta_reader_assertions` must be present, including the
   ones marked `expected_failure`, so a marker can't pass an edge that no
   generation produced.
4. It commits `delta/` on top of the base. Today that is a trailing comment in
   `app/main.go`, so no function moves, and one spec value in
   `deploy/claim.yaml`. A second sync then produces a real cumulative delta,
   which is drained once.
5. The final gate invocation runs the phase at stage `delta`. Plain edges
   must survive. Edges marked `expected_failure` (with `#N` and a reason) must
   stay lost. An edge that survives anyway is a required failure that tells
   the fixing PR to delete its marker.

The leg brackets itself out of the `graph_query` timing phase.

To capture the RED of the open defects, run the gate with
`GATE_DELTA_READER_IGNORE_EXPECTED_FAILURES=1`. That flag asserts every edge
as plainly required. To change the fixture, edit `base/` or `delta/` and update
the snapshot section in the same change. Keep `delta/` under half of `base/`'s
files, or the growth valve turns the second sync into a full snapshot.
`scripts/lib/test-golden-corpus-delta-reader.sh` checks that rule and the
repository id against the synthesized remote. Readers the fixture cannot
express are listed under `unexpressed_readers`, each with its defect and the
reason, and every run reports them.

## Operator signals

- `eshu_dp_projector_delta_union_total` by closed `outcome`
  (`union_base_found`, `union_base_missing_refused`, `union_resolve_failed`,
  `union_scope_mismatch`). A non-refusal lookup error counts
  `union_resolve_failed` and retries through the generic fail path; a
  resolved union whose delta carries no `delta_generation` fact counts
  `union_scope_mismatch` with a WARN, and the projector refuses it before any
  write (#8779).
- `eshu_dp_projector_delta_union_facts` histogram of base/delta/union fact
  counts by `outcome` and `kind`. Counts are values, never labels; commit
  SHAs, scope ids, and generation ids never become labels.
- One `projector.delta_union` span event and one INFO log per union decision,
  carrying `delta_baseline_commit_sha`, `delta_union_base_commit_sha`, and
  `chained` (the delta was diffed over a prior delta: its fence commit is not
  its union base's commit). The `build_projection` stage log carries `chained`
  too. Chained deltas projecting show as `union_base_found` rising while
  `eshu_dp_projector_delta_baseline_fence_total{outcome="refused_base_missing"}`
  and `eshu_dp_collector_reconciliation_full_snapshots_total{reason="repository_reindex_requested"}`
  stay flat.
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
