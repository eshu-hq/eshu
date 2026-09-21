# #6782 slice 3: differential oracle — normalization, kinds, allowlist

## Re-proof legs (post normalization+fixed-dir work)

Both legs ran `scripts/verify-golden-corpus-gate.sh` with
`ESHU_DIFFERENTIAL_CAPTURE=1`, fixed `ESHU_REPOS_DIR=/tmp/eshu-diff-corpus`
(wiped between legs), and per-leg capture dirs.

- NornicDB leg: PASS, 175s (`/tmp/diffproof4-nornicdb.log`, `NORNIC_EXIT:0`).
  2680 records in `/tmp/diff-capture4/nornicdb/`.
- Neo4j leg: PASS, 252s (`/tmp/diffproof4-neo4j.log`, `NEO_EXIT:0`).
  2680 records in `/tmp/diff-capture4/neo4j/`.

Backend pins (handoff): NornicDB
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`,
Neo4j `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`.

## Divergence archaeology (all on the re-proof captures)

- Baseline normalized compare: **1332** unexcused.
- After `_eshu_*` + `generation_id` normalization and fixed corpus dirs:
  **426** unexcused, 90.3%/93.6% records paired.
- After kind decomposition + UNWIND explosion + tier scoping: **671**
  (explosion converts batch diffs into precise per-element diffs).
- After 53-entry allowlist + IN-list explosion: **56** unexcused on the
  pre-normalization captures (39 results-kind on stale digests, 16 missing
  deployment-772 groups whose middles normalize post-legs, 1 rowcount).

## Root causes proven from the captures

1. `resolved_id` embeds the run generation
   (`deployable-unit-correlation:<gen>:<key>`, proven at
   `go/internal/reducer/deployable_unit_correlation_edges.go:399`).
   Normalized by middle segment under the `resolved_id` key only; bare
   content hashes never match the rule prefix shape.
2. `evidence-artifact:` keys are `sha1(resolvedID|kind|path|value)` (proven at
   `go/internal/storage/cypher/edge/writer/row_metadata.go:148`), hence
   disjoint per run by construction. Never normalized in fingerprints.
3. Cross-generation `resolved_<hex>` edge ids are `sha1` over the generation
   plus endpoints (`go/internal/relationships/models.go:231`), so they are
   un-invertible run lineage. Blind in digest rows only.
4. Backend-conditional write emission: NornicDB runs phased MATCH+SET /
   decomposed upserts, Neo4j runs single-statement semantic upserts
   (`go/internal/storage/cypher/semantic_entity_statements.go` vs the
   canonical phased writer). Identical element sets attempted (Function
   78/78, TypeAnnotation 118/118, Module 16/16 overlap measured).
5. Backend-divergent read texts for the same logical reads, proven by shared
   entity bindings (e_59c56c38911d, e_bf45e093fd7d): uid-anchored NornicDB
   reads vs id-OR-uid Neo4j reads, plus a GQL `SHORTEST 1..2` vs 1-hop
   traversal pair. Tracked in #6906.
6. Count-only differences (41/42 groups with equal digest sets): poll and
   retraction iteration counts. Kind-scoped, never result-scoped.
7. Repo sets (32/32) and scope sets (31/31) identical across legs;
   correlation write attempts differ symmetrically by completion order while
   paired correlation reads agree — scheduling, not backend divergence.

## Gate changes (this slice)

- `backendconformance`: divergence kinds, UNWIND + single-use IN-list
  element explosion, `resolved_id`-middle + `artifact_id` normalization,
  digest-row canonicalization (graph objects, list order, clock keys,
  lineage cells). Unit suites green, all pre-existing comparison tests
  preserved (DoubledWrite still fails closed by default).
- `capture`: allowlist tier vocabulary (`statement` wildcard + five kinds),
  kind-scoped matching, executions-tier staleness exemption.
- `specs/backend-divergence-allowlist.v1.yaml`: 53 entries (28 dialect
  reads → #6906, 6 phased writes + 10 correlation/sweep + 9 executions →
  #6782), each with exact statement text, tier, reason, upstream, owner.

## Pending at write time

- Fresh B-7 legs with the capture-side normalization, then the final
  compare: deployment-772 groups should pair (middle-norm), digest
  canonicalization should collapse the results-kind residual.
- Known triage targets post-legs: `r.id IN $repo_ids` repo_name reads (6
  bindings, scalar rows), `w.id IN $ids` workload counts (9 bindings),
  one INVOKES_CLOUD_ACTION rowcount group (6 vs 8 rows, equal digests).
