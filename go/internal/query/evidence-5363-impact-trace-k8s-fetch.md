# Evidence: #5363 prove-the-theory-first — widen the impact-trace k8s SELECTS candidate fetch

Scope of the *planned* change (production code NOT yet written — this file is
the mandatory theory proof that gates it): `go/internal/query/impact_trace_deployment_resources.go`,
`go/internal/query/impact_trace_deployment_k8s.go`, and the shared matcher in
`go/internal/query/content_relationships_k8s_match.go`.

## What #5363 proposes to change

Today `fetchK8sResourceResult` (`impact_trace_deployment_resources.go`) fetches
the impact-trace k8s candidate pool with a single **name-anchored** query,
`SearchEntitiesByName(repoID, "K8sResource", workloadName, serviceStoryItemLimit+1)`
(`WHERE repo_id=$1 AND entity_name ILIKE '%name%' AND entity_type='K8sResource'
ORDER BY relative_path, start_line LIMIT 51`). A Service only enters the pool if
its **name** ILIKE-matches the traced workload's name, so a Service that
`spec.selector`-matches the Deployment's pod-template labels but is named
differently is silently missed — a false-negative SELECTS edge.

Fable's locked design ("anchored directed match"): **keep** the name-anchored
surfaced pool, and **add** a matcher-only Service candidate scan via
`ListRepoEntitiesByType(repoID, "K8sResource", repositorySemanticEntityLimit+1)`
(= 5001, cap 5000). Only Services that actually `selector`-match the traced
Deployment (via the existing `k8sSelectMatch`) join the surfaced pool.

Two theories gate that build. Both were proven with the cheapest representative
shim **before** any production code, per Mandatory Prove-The-Theory-First.

## Result summary

| Theory | Claim as stated | Result | Disposition |
| --- | --- | --- | --- |
| 1 (SQL) | added type-scoped fetch p95 is single-digit ms on worst case, comparable to #5343 | ~25–28 ms at the 5001 cap on a K8s-dominated repo; sub-ms on typical repos | **Claim-as-stated DISPROVEN / design still viable** — see below |
| 2 (matcher) | directed match well under 10 ms/op; all-pairs is disproven Option A | directed (pre-parsed) 5.57 ms/op; all-pairs 4548 ms/op | **PROVEN** (directed) / Option A **DISPROVEN** and recorded |

## Machine / backend profile (resource-qualified)

- `machine_profile`: MacBook Pro, Apple M4 Pro, 12 logical CPU, 64 GiB, SSD, macOS 26.5.2.
- Postgres: `postgres:16` in Docker (`PostgreSQL 16.14` aarch64), throwaway
  container `eshu-5363-explain-shim` on non-default host port `55432`, no
  persistent volume, `docker rm -f` after the shim ran.
- Go bench: `go1.26.5 darwin/arm64`, `cpu: Apple M4 Pro`.
- `absolute_target_applicable`: false — these are relative shim measurements to
  gate a design decision, not a reference-profile wall-clock target.

---

## SHIM 1 — SQL cost (EXPLAIN ANALYZE, worst-case partition)

### Schema and seed

Throwaway `postgres:16` container seeded with the production `content_entities`
schema and indexes mirrored from
`go/internal/storage/postgres/migrations/004_content_store.sql`,
`035_content_entities_repo_entity_idx.sql`, and
`062_content_entity_name_trgm_index.sql`
(`content_entities_repo_idx`, `content_entities_type_idx`,
`content_entities_path_idx`, `content_entities_repo_entity_idx`,
`content_entities_source_trgm_idx`, `content_entities_name_trgm_idx`). As #5343
recorded, there is **no composite `(repo_id, entity_type)` index**.

Worst-case partition for #5363 (which differs from #5343's 1%-share worst case):
`repo-1` holds **10,000 rows, of which 6,000 are `K8sResource`** (3,000 Service
+ 3,000 Deployment, realistic JSONB metadata: `kind`, `namespace`,
`qualified_name`, `selector` on Services, ~20-pair `pod_template_labels` +
`container_images` on Deployments) plus 4,000 non-`K8sResource` filler. Noise
repos `repo-2` (5,000 Function) and `repo-3` (3,000 Variable) bring the table to
18,000 rows. Because #5363's design fetches **all** K8sResource in the repo up
to the 5001 cap, the true worst case is a K8s-dominated repo that **hits the
cap** — this seed returns 5001 of the 6000 K8s rows.

### NEW — the added type-scoped fetch (`ListRepoEntitiesByType`, LIMIT 5001)

```
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
       start_line, end_line, coalesce(language,''), coalesce(source_cache,''), metadata
FROM content_entities
WHERE repo_id = 'repo-1' AND entity_type = 'K8sResource'
ORDER BY relative_path, start_line, entity_id
LIMIT 5001;
```

```
Limit  (actual time=26.278..26.759 rows=5001 loops=1)
  Buffers: shared hit=449
  ->  Sort  (actual time=26.277..26.612 rows=5001 loops=1)
        Sort Key: relative_path, start_line, entity_id
        Sort Method: top-N heapsort  Memory: 4392kB
        ->  Index Scan using content_entities_repo_idx on content_entities
              (actual time=0.011..1.660 rows=6000 loops=1)
              Index Cond: (repo_id = 'repo-1'::text)
              Filter: (entity_type = 'K8sResource'::text)
              Rows Removed by Filter: 4000
Planning Time: 0.355 ms
Execution Time: 26.968 ms
```

Server execution over 10 warm runs: **24.4–28.1 ms** (p50 ≈ 24.8 ms, p95 ≈ 28 ms).
Raw psql `\timing` wall-clock incl. client fetch of 5001 wide rows: 28.2–29.8 ms.

**Cost attribution** (why it is not single-digit): the index scan + type filter
over 6000 rows is only **2.0 ms**; the cost is the **top-N heapsort of up to
5001 wide rows carrying the `metadata` JSONB** through the sort.

| Query variant on the worst-case partition | Execution time |
| --- | ---: |
| Scan + `entity_type` filter only (count, no sort) | 2.0 ms |
| NEW shape, narrow projection (no metadata carried through sort), LIMIT 5001 | 12.5 ms |
| NEW shape, full projection (metadata carried through sort), LIMIT 5001 | ~25 ms |
| NEW shape, full projection, **moderate** partition (100 K8s rows) | 0.256 ms |

### OLD — the existing name-anchored surfaced-pool fetch (`SearchEntitiesByName`, LIMIT 51)

The OLD path is **kept** by the design; measured on the same partition for
context (it is not the thing being added):

```
-- exact workload name: WHERE repo_id='repo-1' AND entity_name ILIKE '%deploy-0001%'
--   AND entity_type='K8sResource' ORDER BY relative_path, start_line LIMIT 51
Index Scan using content_entities_repo_idx ... Rows Removed by Filter: 9999
Execution Time: 3.439 ms   (rows=1)

-- broad ILIKE '%deploy%' (matches 3000, capped at 51) -> planner uses path_idx + incremental sort
Index Scan using content_entities_path_idx ...
Execution Time: 0.058 ms   (rows=51)
```

### Theory 1 verdict: claim-as-stated DISPROVEN; design still viable

The literal claim — "single-digit ms, comparable to #5343 (0.449 ms)" — is
**false at the 5001 cap**: the added fetch is **~25–28 ms p95**, ~55× the #5343
number. The reason is structural, not a mistake in #5343: #5343's worst case was
a **1%-share** repo returning only 200 rows, whereas #5363's design deliberately
returns the full **5001-row cap** on a K8s-dominated repo, so the two numbers
were never comparable.

However the fetch is **not expensive in the ordinary regime**: a repo with a few
hundred K8s resources returns in **sub-millisecond** time (0.256 ms for 100
rows), fully comparable to #5343. The worst-case ~25 ms is a **bounded,
well-understood, self-inflicted** cost — the top-N heapsort carries the wide
`metadata` JSONB purely for deterministic truncation, which the matcher does not
need. **Saved-implementation insight for the executor:** project only the
matcher-relevant columns (`entity_id`, `entity_name`, `kind`, `namespace`,
`selector`, `pod_template_labels`, and the tiebreak keys) — or drop the ORDER BY
below the cap — to bring the cap case toward the 12.5 ms narrow-sort number or
lower. This is a design refinement, not a blocker: even the unrefined 25 ms is a
one-time bounded read inside an already multi-read impact-trace call, on a
pathological monorepo. The coordinator should decide whether to require the
narrow projection in the same PR before dispatching the executor.

---

## SHIM 2 — matcher cost (Go microbenchmark)

Throwaway benchmark (`go/internal/query/zz_shim5363_bench_test.go`, deleted
after this shim) in `package query`, using the real `k8sSelectMatch`,
`k8sSelectMatchInputFromRow`, `parseK8sLabelPairs`, and `buildK8sRelationships`.
Data: **5000 candidate rows all `kind=Service`** with 6-pair selectors, plus
**3 traced Deployment rows** each carrying ~20 pod-template labels. All rows
share `namespace=ns-0` (the matcher **worst case**: every directed comparison
runs the full subset parse, no early namespace short-circuit). Randomness is
varied **by index**, never `math/rand`.

```
go test ./internal/query -run '^$' -bench 'Shim5363' -benchmem -benchtime=50x
goos: darwin  goarch: arm64  cpu: Apple M4 Pro  go1.26.5

BenchmarkShim5363DirectedPreparsed-12   50    5570536 ns/op     6847588 B/op    90032 allocs/op
BenchmarkShim5363DirectedMatch-12       50   16127516 ns/op    30253443 B/op   165044 allocs/op
BenchmarkShim5363AllPairs-12             5 4548448725 ns/op  1231801737 B/op 125210824 allocs/op
```

| Design | What it measures | Calls | ns/op | ms/op | allocs/op |
| --- | --- | ---: | ---: | ---: | ---: |
| NEW directed, pre-parsed (Fable's design) | workload labels parsed ONCE per workload; each Service parses only its own selector | 3×5000 = 15000 | 5,570,536 | **5.57** | 90,032 |
| NEW directed, via `k8sSelectMatch` as-is | same 15000 calls, but the matcher re-parses the 20-label workload map every call | 15000 | 16,127,516 | 16.13 | 165,044 |
| OLD all-pairs (Option A) | all 5003 rows through `buildK8sRelationships` double loop | ~25M | 4,548,448,725 | **4548** | 125,210,824 |

### Theory 2 verdict: PROVEN (directed); Option A DISPROVEN and recorded

- **Fable's directed design (pre-parse pod-template labels ONCE): 5.57 ms/op —
  under the 10 ms/op bar.** PROVEN. The design's stated pre-parse is
  load-bearing: it is what keeps directed under 10 ms.
- **Executor note:** calling the current `k8sSelectMatch` unchanged in the
  directed loop costs **16.13 ms/op** because `k8sSelectorSubsetOf` re-parses the
  20-entry workload label string on *every* candidate. To hit the 5.57 ms number
  the implementation MUST parse each traced workload's `pod_template_labels`
  **once** (hoist it out of the per-candidate loop), exactly as the design says —
  do not naively reuse `k8sSelectMatch` per candidate.
- **All-pairs Option A: 4548 ms/op (4.55 s), 125M allocs — DISPROVEN.** ~816×
  slower than pre-parsed directed and ~282× slower than even the unhoisted
  directed loop. Recorded here as the on-file disproof so no future agent
  re-proposes feeding the whole candidate set through `buildK8sRelationships`.

## Hypothesis ledger

| candidate | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | --- | ---: | ---: | --- | --- | --- |
| T1: type-scoped fetch cheap at 5001 cap | EXPLAIN ANALYZE, 6000-K8s repo | OLD name-anchored 0.06–3.4 ms | NEW 25–28 ms (cap) / 0.26 ms (typical) | N/A read | read-only | claim-as-stated **rejected**; design viable with narrow projection |
| T2a: directed pre-parsed matcher < 10 ms/op | Go bench, 5000 svc × 3 dep | — | 5.57 ms/op | matches=3 asserted | pure CPU | **proven** |
| T2b: directed via k8sSelectMatch as-is | Go bench | — | 16.13 ms/op | matches=3 asserted | pure CPU | proven-but-must-hoist-parse |
| T2c: all-pairs Option A | Go bench, 5003 rows | 4548 ms/op | — | ≥3 rels asserted | pure CPU | **rejected** (on-file disproof) |

## No-Observability-Change (theory-proof phase)

No-Observability-Change: the SHIM phase above is a measurement-only theory-proof
artifact. No production spans/metrics/logs were added or removed while proving
the theory; no production code changed. The throwaway shim container and
`zz_shim5363_bench_test.go` were removed; that phase's committed diff was this
evidence file only.

---

## Finished-change local proof (#5363 implementation)

This section records the proof of the *implemented* change (production code now
written), separate from the theory proof above, per Mandatory Pre-PR Local Proof.

### Correctness — failing-then-green regression

- `TestImpactTraceK8sSelectWideningUnderLinkingRegression` FAILS on `origin/main`
  (proven by copying the test into a throwaway `git worktree` at `origin/main`:
  the differently-named selector-matching Service `web-svc` is never surfaced —
  `rows` contains only the anchored Deployment) and PASSES on this branch. The
  full proof matrix (widening, #5343 false-positive non-regression, namespace
  strictness, pool purity at 5000 candidates, selector-absent + mixed-vintage
  tri-state safety, frozen sub-surface, truncation disclosure + counter) is green
  in `go test ./internal/query`.

### Performance Evidence — final narrow SQL + hydration + matcher

Same machine/backend profile as SHIM 1/2 above (Apple M4 Pro, `postgres:16` in
Docker, throwaway container `eshu-5363-explain-shim` on host port `55433`, no
persistent volume, `docker rm -f` after the run; same 18,000-row seed:
`repo-1` = 6,000 K8sResource (3,000 Service + 3,000 Deployment with a 22-pair
`pod_template_labels`) + 4,000 filler, plus `repo-2`/`repo-3` noise).

Benchmark Evidence:

- **Final `ListRepoK8sSelectCandidates` SQL** (the exact production query,
  including the `coalesce(jsonb_typeof(metadata->'key') = 'string', false)`
  tri-state presence columns), `LIMIT 5001`, 10 warm runs on the 6,000-K8s
  worst-case partition: **p50 ≈ 7.6 ms, p95 ≈ 9.16 ms** server `Execution Time`.
  Plan: `Index Scan using content_entities_repo_idx` (6,000 rows, 4,000 removed
  by the `entity_type` filter, 2.2 ms) → `quicksort` of narrow 166-byte-width
  rows (2,102 kB). This is the narrow-projection win the shim predicted: the
  wide-projection variant top-N heapsorted the metadata JSONB at ~25 ms; the
  final narrow shape is **≤ 15 ms p95** as required (no index added — #5490).
- **Hydration `ListRepoEntitiesByIDs` SQL** (`entity_id = ANY($2)`, 5 matched
  IDs), 5 warm runs: **≈ 0.07 ms** server `Execution Time` — well under the
  ≤ 2 ms bar (PK/`content_entities_repo_entity_idx` lookup, no sort cost).
- **Committed matcher benchmark** `BenchmarkK8sWorkloadMatchTargetDirectedScan`
  (prepared target parsed ONCE, 5,000 candidate Services, 20-label workload, all
  same namespace — matcher worst case): **1.57 ms/op**, 15,005 allocs/op on
  `go1.26 darwin/arm64`, Apple M4 Pro — under the 10 ms/op bar and consistent
  with the shim's 5.57 ms/op directed-pre-parsed result (the shim carried three
  workloads; this loop carries one). This is the enforcement of the D2 pre-parse
  requirement; there is no CI wall-clock assert (flake generator).

No-Regression Evidence: the change is additive on the surfaced pool. For a repo
with no new selector match the surfaced `rows`, `image_refs`, and every
pre-existing `k8s_resource_limits` value are byte-identical (asserted by
`TestImpactTraceK8sSelectWideningFrozenSubSurfaceOnNoMatch`); the directed scan
is skipped entirely when the anchored pool has no Deployment target, so the
common path pays nothing.

### Observability Evidence

Observability Evidence: the truncation of the directed SELECTS candidate scan is
now disclosed on the impact-trace surface. On a sentinel hit,
`fetchK8sSelectMatchedServiceIDs` sets `k8s_relationships_complete=false` with
reason `k8s_select_candidate_pool_truncated` in the `k8s_resource_limits` map
(both `k8s_relationships_complete` and `k8s_select_candidate_sentinel_limit` are
always present so clients can read completeness on every response), emits a
`WarnContext` log carrying `repo_id`, and increments the existing
`eshu_dp_query_k8s_select_candidate_scan_truncated_total` counter with the single
bounded `reason` attribute (`repo_id` is deliberately kept out of the metric to
stay low-cardinality). Asserted end-to-end by
`TestImpactTraceK8sSelectWideningTruncationDisclosure` via a manual metric
reader. Concurrency: both new reads are read-only; no lock/lease/queue path is
touched, so no concurrency proof is required.
