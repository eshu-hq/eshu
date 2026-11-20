# #10878 theory proof: AWS drift changed-ARN delta (first domain)

Refs #10878. Part of #10860 (phase 2, step 4). Design: arbiter ruling
(decision 1, slice order: AWS first) + prior evidence (issue figures,
#10861-D3, #8283 fingerprint contract, #10875 proof shape).

Prove-the-theory-first shim. No implementation here: numbers from a
throwaway live-Postgres shim (deleted after the run; Go source kept only in
`/tmp/10878-shim/` outside the repo) plus the fixture plan the
implementation lane will build. No repo code changed by the shim.

## Method

Disposable PostgreSQL 18-alpine on loopback (own container, destroyed after
the run). Minimal `fact_records` + `ingestion_scopes` shape with the
production index set (ARN expression index incl. migration-180 state equivalent,
`(scope, generation, kind)`). ~1KB payloads (prod is ~3KB, so load legs are
biased *against* the delta — conservative).

Seed, two corpus sizes: N=2000 and N=8000 AWS resources in `genB`, `genA` =
same set with ~1% payload-changed (v1→v2), 0.5% added, 0.5% removed;
`terraform_state_resource` facts for ~70% of ARNs across 10 state scopes with
active generations. Load legs use the production SQL text verbatim
(`listAWSCloudRuntimeResourcesForGenerationQuery`,
`listActiveStateResourcesForAWSARNsQuery`). 2 warmups + 7 reps,
`EXPLAIN (ANALYZE, BUFFERS)` medians, hot cache.

The diff leg uses a save-time `(arn, phash)` identity column standing in for
the fingerprinted signal the design records at fact-save time (#8283
`reopen_fingerprint`, #10875 sidecar precedent).

## Verdict: PROVEN with one design correction

Row-set equality is exact at both sizes (0 mismatches every leg):

| Check | N=2000 | N=8000 |
| --- | --- | --- |
| E2: every genB ARN carried or in diff | 0 unaccounted | 0 unaccounted |
| E3: delta state rows == full state rows on changed ARNs | 0 mismatch (20 rows) | 0 mismatch (100 rows) |
| Probe leg == FULL-A payloads on changed ARNs | 0 mismatch | 0 mismatch |
| Candidate state leg == production DELTA-B row set | 0 mismatch | 0 mismatch |

`BuildCandidates` is per-ARN independent (sort + per-row `Classify` +
per-row build; `candidate.go:79-105`), so equal `AddressedRow` inputs imply
equal candidates; removals flow to the existing `EvaluatedARNs`-bounded
retire. Cost (medians):

| N | Full redo (A+B) | Naive delta (diff + prod state query) | Ratio |
| --- | --- | --- | --- |
| 2000 | 11.18 ms | 2.84 ms | 25.4% |
| 8000 | 67.35 ms | 24.47 ms | 36.3% |

The naive delta **fails** the 25% bar and its ratio *grows* with N. Root cause
(plan captured): the production `aws_generation_arns` CTE rechecks
`requested.arn = fact.payload->>'arn'` as a full-generation hash recheck —
at N=8000 the planner scans all 8,040 genB rows (1,132 heap blocks,
~19.6 ms fixed) even for 160 requested ARNs. That fence is correct for full
loads (stale caller input cannot widen the join) but punitive per changed ARN.

Restructured delta (diff + per-ARN LATERAL membership probe through the ARN
expression index + direct per-ARN state probe through the state ARN index,
active-generation join kept as the staleness fence):

| N | Restructured delta | Ratio | Per changed fact: full → delta |
| --- | --- | --- | --- |
| 2000 | 1.43 ms | 12.8% | 0.280 ms → 0.071 ms |
| 8000 | 4.90 ms | 7.3% | 0.421 ms → 0.153 ms |

**The theory holds iff the delta probes per changed ARN through the existing
expression indexes and does not reuse the full-generation recheck CTE.**
Ratio improves with N (grows with changed, not total). The safety property
moves, not away: membership is proven per (scope, generation, ARN) by the
probe; the active-generation join still fences stale state.

Rejected with a number: per-run payload rehash diff = 11.48 ms at N=2000
(~the full load itself). Save-time content identity is required, not optional.

## What this does not prove (named gaps for the implementation lane)

- Real changed-ARN fraction at QA scale (shim: 2%). Projection from the
  restructured shape: fixed ~4 ms + ~0.005 ms/changed ARN at N=8000 scale,
  so the 25% bar holds until ~25% churn; re-measure on the golden corpus.
- Snapshot-completeness: proven only at the writer-contract level
  (generation-authoritative retire + `allow_empty_successor` attestation,
  `aws_cloud_runtime_drift_writer_queries.go:11`). Collector-side
  attestation re-verification is owed before diff-as-removal ships.
- No-change runs (the dominant win: fingerprint match → skip): not timed
  here; by design one indexed fingerprint-row compare. Slice 1 (reopen gate)
  carries that win and proceeds regardless.
- Per-domain p95 for audit budgets, QA histogram read: NOT_CHECKED (no
  read-only QA DSN on this box).
- Config-load leg (`loadConfigByStateScope`) not modeled: unchanged ARNs
  skip it entirely by construction; changed ARNs pay it as today.

## Fixture plan for the golden-corpus differential (AC2)

Full-vs-incremental on identical seed, row-set/edge diff 0/0 both directions,
plus match / no-match / ambiguous cases and one removal each:

- Should match: converged ARN (identical payload both gens) → no candidate
  either way; stable orphaned (cloud-only); stable unmanaged with same
  owner; ambiguous multi-state-owner → `ambiguous_cloud_resource` both ways.
- Should not match: v1→v2 value drift → identical drift candidate both
  ways; added ARN → new finding in both; removed ARN → retired via the
  `EvaluatedARNs`-bounded retire in both; moved state ownership (address
  change) → identical reclassification both ways.
- Unclear (semantics to pin before build): `Inconclusive()` value
  comparison (Compared==0 / degraded) — delta must carry the prior finding
  forward, never retire it (#5837/#5861 hazard); mid-ingestion
  `state_snapshot` generation — delta must defer via the readiness gate, not
  write `orphaned_cloud_resource`; empty successor with
  `allow_empty_successor=false` — fail-open full run (counted) vs defer;
  conflicting state rows appearing/disappearing between runs.

## Figures for AC6 (shim scale, hot cache, ~1KB payloads)

Working ms per changed fact: 0.280 → 0.071 (N=2000), 0.421 → 0.153
(N=8000). Against the #10862 baseline (35.7 s avg/run over 771 runs): the
per-run average divides by changed facts only once the design's
`changed_keys` counter lands — that instrumentation is part of the slice,
not of this shim. Absolute QA-vs-disposable times are not equated
(different hardware, no classification/write/contention modeled).
