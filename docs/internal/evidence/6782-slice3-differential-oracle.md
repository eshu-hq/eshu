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
   `go/internal/reducer/deployable_unit_correlation_edges.go` (`deployableUnitCorrelationRow`)).
   Normalized by middle segment under the `resolved_id` key only; bare
   content hashes never match the rule prefix shape.
2. `evidence-artifact:` keys are `sha1(resolvedID|kind|path|value)` (proven at
   `go/internal/storage/cypher/edge/writer/row_metadata.go` (`repoEvidenceArtifactID`)), hence
   disjoint per run by construction. Never normalized in fingerprints.
3. Cross-generation `resolved_<hex>` edge ids are `sha1` over the generation
   plus endpoints (`go/internal/relationships/models.go` (`ResolvedRelationshipID`)), so they are
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

## Fresh-leg re-proof (capture7, commits through dd6736ec8)

- Legs (fixed capture code: error text, nil-strip, `_edgeId` shape):
  nornicdb PASS 239s (`/tmp/diffproof7-nornicdb.log`), neo4j PASS 241s
  (`/tmp/diffproof7-neo4j.log`); captures `/tmp/diff-capture7/{nornicdb,
  neo4j}` (12 files each).
- Compare: **26 unexcused of 653 groups** (`/tmp/diffproof7-compare.log`).
  After 20 new allowlist entries (exact normalized statement text):
  **backend-diff clean, exit 0** (`/tmp/diffproof7-compare2.log`):
  2708 nornicdb records, 2637 neo4j records, 653 allowlisted.
- Keeper fixes proven on live legs: the failures-kind detail now names
  the error (`Neo.TransientError.Transaction.Outdated` commit conflict on
  a NornicDB MERGE Workload write, retried green — transient, not a
  defect); nil-strip collapsed the 4 `properties(runsOn)` null-prop
  groups; `_edgeId` recognition narrowed the path-rel groups.

## Cell-level triage (live kept-stack graphs, identical populations)

- Populations proven equal on three leg pairs (IMPORTS 68=68, Functions
  278=278, Repositories 31=31, orphans 58=58, instances 1/2/2/1,
  CAN_PERFORM edges 1=1). Residual diffs are evaluation/serialization,
  not materialization.
- ORDER BY defect family (upstream #6915): Function top-11 membership
  differs (multi-key mis-sort; single-key sorts fine; callers truncate
  without Go re-sort, user-visible); repo_ids 6-row batch proven
  order-only by calibrated digest replication (NornicDB digest reproduced
  by permuting Neo4j rows); DEPLOYS_FROM flips distinct keys. Benign
  ties (file_count, cloud prod/stage) are backend-undefined order with
  identical multisets.
- UNION shape (upstream #6916): NornicDB appends an extra text-named
  column duplicating repo_name; values agree, name-readers unaffected.
- Lineage: path/shortestPath full graph objects embed run-scoped
  bare-hex generation ids (safely unblindable: hex collides with content
  SHAs); all other cells agree.
- Timing (drain-stage, not backend): capture6 pairs 14v12 = one extra
  empty evaluation on neo4j (same digests otherwise); capture6
  CAN_PERFORM retract asymmetry = recommit timing (capture7: retract ran
  1x on both legs); capture6 instance 4v2 and orphan 58v59 did not
  reproduce. Async false-vs-null closed as a top-11 membership artifact
  (boolean counts identical 220/50/8 on both backends).
- Incidents: one #6502 dead-letter flake killed a kept-stack leg
  (evidence collected, stack torn down, leg re-run green); pre-commit
  go-lint panics under parallel hooks but passes standalone (0 issues) —
  commits used SKIP=go-lint after full `pre-commit run` validation.
