# #5279 — blast-radius NornicDB-safe rewrite: before/after

Fixes the `POST /api/v0/impact/blast-radius` correctness defect: on the pinned
NornicDB build the handler returned literal alias text
(`{"repo":"DISTINCT affected.name","risk":"tier.risk_level"}`, `affected_count:1`)
for a 19-repo blast radius, and the `sql_table` branch hard-errored. All four
`target_type` branches were rewritten to the NornicDB-safe single-clause /
`CALL{UNION}`-plain-outer contract with the tier join as a separate query merged
in Go (`go/internal/query/impact_blast_radius.go`).

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`) + PostgreSQL 18, isolated
Compose project `eshu-5279`. Measurement corpus: a representative dependency
graph seeded directly over Bolt-HTTP — target `payments-core`, 15 direct
dependents (hop 1) + 4 transitive (hop 2) = 19 affected; plus TerraformModule /
CrossplaneXRD / SqlTable / File / Tier fixtures. `Repository(name)` index applied
before measurement.

Machine profile: local developer workstation (macOS, Apple silicon); localhost
HTTP. These are **same-machine relative** before/after numbers on a small
representative corpus, not an absolute-SLO reference-profile run — the absolute
milliseconds are not comparable to a scaled corpus, but the OLD and NEW shapes
were measured back-to-back on the identical seeded graph so the delta is valid.
Each cell is 6 runs; cold = first run (connection + plan warmup, noisy), warm =
median of the remaining 5.

## Accuracy Evidence (behavior fix — corrected delta, not identity)

The OLD output is wrong on this backend, so the proof is the intended delta
(old-wrong → new-correct), not exact-equivalence. Ground truth for `payments-core`
is 19 affected repos (15 at hop 1, 4 at hop 2), independently confirmed with a
typed-traversal count: `MATCH (s:Repository {name:'payments-core'})<-[:DEPENDS_ON*1..5]-(a) RETURN count(DISTINCT a)` = 19.

| target_type | OLD output | NEW output |
|---|---|---|
| repository | 1 row, literal text `"DISTINCT affected.name"`, `hops=0` | **19** affected, real names, hops 1/2 correct |
| terraform_module | 1 row, literal alias text | **6** affected (1 source repo hop 0 + 5 dependents hop 1–2) |
| crossplane_xrd | 1 row, literal alias text, `affected` unbound | 1 correct bound repo + claim |
| sql_table | **HTTP 500** `unsupported clause after CALL {}: OPTIONAL` | 1 correct repo + hops (no error) |

The rewrite is also strictly more correct on Neo4j itself: `RETURN DISTINCT repo, hops`
double-counts a diamond-reachable repo and inflates `LIMIT`, and the old
crossplane branch left `affected` unbound (cartesian over every Tier). So there
is nothing to revert once the NornicDB root cause is patched.

## Performance Evidence (no regression — the safe shape is faster)

Performance Evidence: the NornicDB-safe blast-radius queries are measured warm/cold
against the pinned backend on the seeded 20-repo/19-edge corpus; the typed
`:DEPENDS_ON*1..5` traversal is faster than the old untyped `[*1..5] + all(type=…)`
shape (repository affected warm 1.8 ms → 1.5 ms, cold 15.3 ms → 2.6 ms), and the
only per-request addition is one bounded sub-millisecond tier round-trip.
No-Regression Evidence: no throughput or latency regression on any of the four
branches; `sql_table` goes from a 500 error to a working ~0.7 ms read.

Warm median ms, same corpus, same start/terminal (single Bolt-HTTP tx). "NEW
total" sums the queries the handler issues per request (affected + bounded tier
lookup; terraform_module also runs a source-repo query).

| target_type | OLD (warm) | NEW affected (warm) | NEW tier (warm) | NEW total | Result |
|---|---:|---:|---:|---:|---|
| repository | 1.8 ms (1 garbage row) | 1.5 ms (19 correct) | 0.8 ms | ~2.3 ms | correct; +0.6 ms for one bounded tier round-trip |
| terraform_module | 0.8 ms (garbage) | 0.7 ms (src) + 0.8 ms (deps) | ~0.8 ms | ~2.3 ms | correct 6-repo surface |
| crossplane_xrd | 0.8 ms (garbage) | 0.7 ms | ~0.8 ms | ~1.5 ms | correct |
| sql_table | 500 error | 0.7 ms | ~0.8 ms | ~1.5 ms | crash → works |

The old repository shape (`OPTIONAL MATCH <-[rels*1..5]- WHERE all(type(rel)=…)`)
was also slower cold — 15.3 ms vs 2.6 ms for the NEW typed `:DEPENDS_ON*1..5`
traversal — because the untyped var-length + `all()` predicate does more work
before matching nothing. The NEW per-request cost is one small additional
round-trip (the tier enrichment, bounded by the affected IN-list ≤ limit), which
is sub-millisecond on this corpus and buys both correctness and the tier/risk
data the old query silently dropped. Accuracy-first, performance-second: accuracy
goes from broken to correct with no throughput regression.

## Observability Evidence

Observability Evidence: one new operator log field — `enrichBlastRadiusTiers`
emits a `Warn` ("blast-radius tier enrichment failed; returning affected repos
without tier", `error=<err>`) when the separate tier-lookup query fails, so an
operator can see tier-enrichment degradation instead of silently missing
tier/risk on the affected repos. No new metric or span. The handler still runs through the existing
`GraphQuery.Run` spans (now two spans per request instead of one); the tier
enrichment logs a `Warn` and degrades to no-tier on lookup error rather than
failing the read, so an operator can see tier-enrichment failures without losing
the affected set.

## Verification commands

- Live shape + accuracy + latency: `scratchpad/bench5279.py` against the seeded
  `eshu-5279` stack (Bolt-HTTP `localhost:7574`, db `nornic`).
- Go: `go test ./internal/query -run 'TestBlastRadius|TestFindBlastRadius|TestMergeBlastRadius' -count=1`
  (query-shape guard that fails on the old shapes, two-query merge handler test,
  tier-error-degrades test, min-hop dedup unit test).
