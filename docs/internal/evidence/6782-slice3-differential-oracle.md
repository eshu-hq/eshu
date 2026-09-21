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
- `specs/backend-divergence-allowlist.v1.yaml`: 53 entries at capture7
  (28 dialect reads → #6906, 6 phased writes + 10 correlation/sweep +
  9 executions → #6782), each with exact statement text, tier, reason,
  upstream, owner. Net at slice end: 53 + 20 triaged − 1 retired
  (entry 57, converged) = 72; see the capture8 appendix below.

## Fresh-leg re-proof (capture7, commits through dd6736ec8)

- Legs (fixed capture code: error text, nil-strip, `_edgeId` shape):
  nornicdb PASS 239s (`/tmp/diffproof7-nornicdb.log`), neo4j PASS 241s
  (`/tmp/diffproof7-neo4j.log`); captures `/tmp/diff-capture7/{nornicdb,
  neo4j}` (12 files each).
- Compare: **26 unexcused of 653 groups** (`/tmp/diffproof7-compare.log`).
  After 20 new allowlist entries (exact normalized statement text; 19 net —
  entry 57 later retired as converged, see appendix):
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

## Re-proof on the fix-490 re-pin (capture8, post #6894)

#6894 moved the default NornicDB image from
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81ced…` to
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb695…`
(upstream conjunct index-seek fix orneryd/NornicDB#491). Branch rebased onto
`origin/main e5fa16636` (merge-base verified). Fresh legs on the new default
(no image override anywhere in env or gate script):

- NornicDB leg: PASS (`/tmp/diffproof8-nornicdb2.log`), 12 files,
  `/tmp/diff-capture8/nornicdb` (2669 records). The first attempt's compare
  showed 2801 unexcused at a 2.02x record ratio — diagnosed as STALE capture
  files (12 files timestamped 10:07–10:11, mode 0644 from a pre-tightening
  binary, alongside 12 fresh 0600 files): the capture dir was not wiped,
  only the corpus dir was. Lesson: wipe the capture dir before every leg.
  Re-ran on a wiped dir; the 2801 vanished.
- Neo4j leg (run 1): PASS (`/tmp/diffproof8-neo4j.log`), 12 files, 2669
  records. Compare (`/tmp/diffproof8-compare3.log`): 48 unexcused (IMPORTS
  count digest shifted on the neo4j side; ~47 per-function
  INVOKES_CLOUD_ACTION 7v8 rowcount with equal digests) plus stale entry 57.
- Entry 57 retired: the `ORDER BY repo_name` repo_ids statement (ex-#6915
  entry) agrees byte-identical on both sides on the new image (6 rows,
  digest `b6dfa2…` both backends, cell proof in captures). The upstream
  fix resolved this instance; the stale guard caught it — differential run
  35632437752 reports `divergence allowlist entry 57 (... ORDER BY
  repo_name): matched no divergence in this run (stale)`. No replacement;
  retirement committed as `c790840e5`.
- Neo4j leg (run 2): PASS (`/tmp/diffproof8-neo4j2.log`), 12 files, 2657
  records. Compare (`/tmp/diffproof8-compare4.log`): the 48 VANISHED
  (IMPORTS and INVOKES agree), leaving 2 unexcused:
  1. CAN_PERFORM sink probe (`UNWIND $pairs ... ORDER BY function_uid,
     sink_rel`): row digest differs, 14 vs 12 rows (results-kind).
  2. Platform finalizer (`UNWIND $rows MERGE (p:Platform ...)`):
     failed executions differ, nornicdb=1 vs neo4j=0 (failures-kind);
     error `Neo.ClientError.Statement.SyntaxError (commit failed:
     constraint violation: UNIQUE on Platform.[id])` — a concurrent-MERGE
     write race, 1 failed execution in 29 on one leg, 0 failures in ~90
     executions across the other three legs.

Fix-490 assessment (A/B across images): the NornicDB side is byte-stable —
IMPORTS count digest `b927e904adc0` identical on old and new images, and the
capture8-nornicdb INVOKES distribution matches capture7-neo4j exactly. All
observed variance sits on run-to-run leg timing (drain/finalization paths),
not on the image change. NO fix-490 regression; one fix-490 repair (entry
57 instance). The 48-set and the 2 residuals never co-occur in one pairing:
every residual is leg-pair-dependent, i.e. scheduling noise by the
multi-leg test, but the results/failures kinds cannot be allowlisted without
flap (non-executions entries go stale on green runs). Disposition of the 2
residuals is an owner policy decision (source-fix vs gate policy); they are
NOT excused in this change.

Burn-down: 53 entries + 20 triaged − 1 retired (entry 57) = **72 total**
(`rg -c -- '- statement:'` at `c790840e5`). Product follow-ups filed:
#6922 (Platform MERGE UNIQUE race, open), #6923 (CAN_PERFORM probe
drain-timing wobble, open). #6915 stays open for the remaining ORDER BY
entries.

## Multi-leg quorum end to end (owner direction 2026-09-21)

CI run 35635362608 went red on 2 Module/orphan divergences the capture8
legs never showed (orphan scan 58v59 rows; candidate probe 7v14 executions
with agreeing digests). Recording-level triage of the uploaded
`differential-capture` artifact traced both to a single extra orphan row on
the neo4j leg: the 59th orphan duplicates the `database/sql/go` candidate
key, and the UNWIND explosion counts it twice per execution (7v14, same
digest). Scheduling noise, not backend divergence — the case for quorum.

Quorum proof with the branch binary (no new legs run):

- Pairing 1 (local capture8: `/tmp/diff-capture8/{nornicdb,neo4j}`, 2669/2657
  records): 2 unexcused — CAN_PERFORM sink probe (14v12 rows), Platform
  MERGE race (1v0 failures). Single-pair mode: exit 1
  (`/tmp/quorum-pair-local.log`).
- Pairing 2 (CI run 35635362608 artifact, 2743/2702 records): 2 unexcused —
  Module orphan scan, Module candidate probe. Single-pair mode: exit 1
  (`/tmp/quorum-pair-ci.log`, `/tmp/quorum-single-recheck.log`).
- Quorum mode over both pairings (`-diff-left2/-diff-right2`): intersection
  empty, exit 0 — required `nornicdb_vs_neo4j_quorum` PASS plus advisory
  `nornicdb_vs_neo4j_nonreproducing` listing all 4 pairing-local
  divergences (`/tmp/quorum-proof.log`: "2 pass, 0 required-fail,
  0 advisory-warn"). Each single pairing stays red on its own; a systematic
  divergence (same fingerprint both pairings) still fails by construction,
  pinned at unit level by `TestQuorumIntersectionKeepsReproducedDivergences`.
