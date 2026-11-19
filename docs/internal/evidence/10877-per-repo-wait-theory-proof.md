# #10877 theory proof: wait only for the repositories a piece of work reads

Refs #10877. Part of #10860 (phase 2, step 3).

Prove-the-theory-first shim for #10877. No implementation here: numbers
from a throwaway live-Postgres shim (container removed after the run) plus
the designs of the proof tests the implementation lane will build. No
production code and no committed test files: the accuracy review and its
fixtures come first per the issue mandate.

Method follows the #10876 step-0 gate shim: disposable `postgres:18-alpine`
on loopback (`eshu-10877-proof-pg`, `127.0.0.1:55499`, removed after the
run), predicate copied verbatim from
`incompleteScopeRelationshipGenerationsPredicate`
(`go/internal/storage/postgres/relationship_schema.go`), rolled-back
`EXPLAIN (ANALYZE, BUFFERS)`, 2 warmups + 7 reps, medians. Seed: 4,000
active scopes with one active generation each, 160,000 resolved rows (40
per scope, source-ownership shaped: source is the owning scope's repo,
target pseudo-random over the pool), production-like indexes
(`rr_source_idx`, `rr_target_idx`, `rg_scope_gen`, `rg_scope_status`,
`ingestion_scopes_pkey`). Absolute times are shim-local (warm cache, small
box) and are reported as ratios; QA absolutes need the coordinator's live
window.

## Criterion 1: what each kind of work reads, and how the set is known

| Kind | Repository set one item reads | Known before the read from |
| --- | --- | --- |
| `workload_materialization` | `workloadCandidateRepoIDs(candidates)`: repos of the scope's own workload candidates | `ExtractWorkloadCandidates` over the scope's own Repository+File envelopes (after the bulk fact load, before the resolved read) |
| `deployable_unit_correlation` | Same candidate repo set | Same extraction; the set is also recorded in `deployable_unit_reopen_fingerprints.repo_ids` at success (#8283) |
| `service_catalog_correlation` | `distinctServiceRepositoryIDs(writes, serviceRepositoryIndex(decisions))` | Correlation decisions + materialization writes (before the read, no bulk fact load on this path) |

The read (`source OR target IN set`) is also the use: `applyResolvedDeploymentSources`
joins on the candidate repo as app side and `groupDeploymentRelationshipsByRepo`
buckets for the same repos, so no consumer reads a repository outside its
named set. The risk the issue names is the reverse direction: a repository
the item did NOT name can still change its answer, because with
source-ownership partition (`partitionResolvedOwnership`) any foreign scope
may emit an edge whose target is one of the item's repos. Measured in (c):
5 of 300 replay items changed with no named-repo overlap.

## (a) Per-repository predicate cost vs the global fence check

Fence-only statements, one holder (`gen-7` retired) unless noted:

| Check | Verdict | EXPLAIN median | Buffers |
| --- | --- | --- | --- |
| Global `NOT EXISTS` (holder present) | incomplete | 0.951 ms | 54 |
| Global `NOT EXISTS` (all complete) | complete | 1.521 ms | 78 |
| Per-repo K=2, holder in set | incomplete | 0.069 ms | 8 |
| Per-repo K=2, holder outside set | complete | 0.108 ms | 18 |
| Per-repo K=10, holder in set | incomplete | 0.316 ms | 57 |
| Per-repo K=10, holder outside set | complete | 0.312 ms | 67 |
| Per-repo K=10, all complete | complete | 0.311 ms | 67 |

Fused fence+read statements (K=10 repos, holder present):

| Check | Verdict | EXPLAIN median | Buffers |
| --- | --- | --- | --- |
| Fused, global fence | incomplete, no rows | 2.977 ms | 845 |
| Fused, per-repo fence (holder in set) | incomplete, no rows | 2.326 ms | 848 |
| Fused, per-repo fence (holder outside) | complete, rows | 2.423 ms | 857 |

Verdict: PROVEN cheaper, and the cost scales with K, not corpus size. The
plan explains why: the global check seq-scans all 4,000 scopes (78
buffers), while the per-repo check bitmap-probes K scope PKs then
index-probes their generations (8 buffers at K=2). On the fused path the
win is smaller (~0.5 ms, ~20%) because the by-repos read dominates (845
buffers either way); the big win is hold frequency (b), not per-statement
time. NOT_CHECKED: the repo-to-owning-scope map lookup
(`ListScopeRepositoryIDs`) the fence restriction needs per item; it is one
indexed read per scope generation, not measured here.

## (b) Hold/release frequency and oldest wait, one unsettled repository

Replay: 300 waiting items (100 per domain), read-sets of 2 repos,
concentrated so every 7th item references the shared deployment repo
`repo-s-7` (15% concentration: one unsettled shared repo holding back the
fleet is the issue's shape), ages spread 0–3.2 h (the issue's oldest
workload wait). Unsettled scope `s-7` retired.

| | Held | Oldest wait |
| --- | --- | --- |
| BEFORE (global fence) | 300/300 | 3.19 h |
| AFTER (narrow wait: holder owns a repo in the set) | 43/300 (14.3%) | 3.14 h among held; 257/300 proceed at once |

Verdict: PROVEN directionally. Holds fall ~7x on this concentration (to
~0% in the uniform limit: 2 random repos over 4,000 scopes names the
holder with p≈0.05%/item). The 43 held items are exactly the ones whose
answer the holder can change source-side; their wait is unchanged, which
is correct. Production before/after against the #10862 baseline needs the
coordinator's live window (issue criterion 4 stays open).

## (c) Late-arrival restart: narrow wait is unsound alone, sound with fingerprints

Same replay: after recording each item's input fingerprint (sha256 of the
sorted `resolved_id` set touching its repos, the #8283 digest shape, active
rows only), the holder `s-7` activates late with its 40 rows.

| | Count |
| --- | --- |
| Fingerprint changed on late activation | 48/300 |
| Changed AND narrow-held (source-side, expected) | 43/48 |
| Changed but NOT narrow-held (target-side catch) | 5/48 (~1.7%) |
| Changed by domain | workload 17, deployable 15, service-catalog 16 (even, as seeded) |

The 5 target-side items are the accuracy finding: a foreign scope's
resolution emits edges pointing AT the item's repos, so waiting only for
owning scopes of the named set would record a partial answer 5 times in
300 with nothing reopening it. The fingerprint catches all 5. Design the
implementation lane must build (no code here):

- Record the repo set + input fingerprint at success for all three kinds.
  Only deployable-unit has it today (`deployable_unit_reopen_fingerprints`,
  migration 193); workload materialization and service catalog need the
  same table shape and record path.
- Reopen on late arrival two ways: the immediate in-transaction wake on
  relationship generation activation (#8294 pattern,
  `ReopenDeployableUnitCorrelationsForRepos`, extended to the two new
  tables) plus the deferred-maintenance changed-evidence listing
  (`ListDeployableUnitReopenCandidatesQuery` pattern) as the audit bound.
- Proof tests (designs, not implemented): both-orders equivalence
  (waiter-first vs input-first converge byte-equal, after #10876's design);
  late-arrival reopen (succeed while H retired, activate H, assert the item
  reopens and converges to the full-input result; assert the 5/300-shaped
  target-side case is in the fixture mix with should-match, should-not-match
  and unclear cases per criterion 2); missed-wake audit (plant a satisfied
  wait, assert the audit reopens within its bound).
- Keep `eshu_dp_corpus_fence_oldest_deferred_age_seconds` until the old
  path is removed (criterion 5), and add per-reason narrow-wait gauges
  beside the #10876 waiting gauges.

## Out of scope (criterion 6)

The quiescence probe (#10871) and the winners watermark (#10706) keep
their global shape in this lane. Recommendation: decide each separately
after this lands — quiescence asks "is anything still running" (inherently
global; per-repo makes no sense) and the winners watermark is one
maintainer (fan-out, not wait-set, is its lever per the #10876 proof).

## Environment notes

Disposable `postgres:18-alpine` container on `127.0.0.1:55499`, removed
after the run. Read-only QA queries: none needed (issue figures plus the
local shim carried every number). Shim scripts live in `/tmp/10877-shim`
(outside the repo, never committed); nothing to review in `go/`.
