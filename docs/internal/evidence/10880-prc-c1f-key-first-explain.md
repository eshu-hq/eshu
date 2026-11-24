# #10880 PR-C1 item f: key-first ungranted explain reads (evidence note)

Branch `drive/10880-prc-c1f` (stacked on C1, off `cc01f11fed`). Ruling: `.claude/arbiter-c1-item-f-2026-11-24.md` (arbiter, 2026-11-24 15:55Z).

## What this changes
The ungranted explain by selector used to read the whole supports table through a `NOT MATERIALIZED` source joined to the active scopes, with the selector
as an optional predicate (`$4 = '' OR cve_id = $4`). On the QA replica the planner drove that join from the scopes and read every supports row through the
scope index with the selector only as a filter: every selector and identity shape reached the 20 s statement timeout on both runs (4 PASS, 18 FAIL of 22).

Now each selector drives from its own index through a `MATERIALIZED` candidate leg, the fence the image route already uses
(`impact/explain_supports.go`, `supportsExplainKeyFences`):

| selector the call names | driving leg | index |
|---|---|---|
| CVE id (`$4`) | `cve_id = $4 AND cve_id <> ''` | `supports_cve_idx` (339) |
| advisory or CVE id (`$3`) | two arms: `advisory_id = $3 AND advisory_id <> ''` UNION ALL `cve_id = $3 AND cve_id <> '' AND advisory_id <> $3` | `supports_advisory_idx` (340), `supports_cve_idx` |
| repository (`$6`) | `repository_id = $6 AND repository_id <> ''` | `supports_repository_fact_idx` (346) |
| package (`$5`) | `package_id = $5 AND package_id <> ''` | `supports_package_idx` (345) |

The route function (`ungrantedExplainDrivingKey`) picks the key in the fixed order CVE, advisory-or-CVE, repository, package. Every other selector, the grant,
the identity predicate and the active-scope join apply to the candidates; the active scopes never drive. The `<> ''` literal repeats each partial index's
predicate so a generic plan can prove the index applies.

## Two lookups stay on the legacy read in this PR
The **subject digest** and the **canonical key of the identity fallback** have no supports index to drive from. A call whose only selector is a subject digest, and an
identity lookup (fact id, finding id, canonical key) that names none of CVE, advisory, repository or package, keeps the legacy read until those indexes exist.

Design note, not code, for the two migrations (arbiter 8.4): one `CREATE INDEX CONCURRENTLY` per migration file, each proved in the lab first, then on the replica
by the same plan-shape test: (1) `subject_digest` partial btree `WHERE subject_digest <> ''` for the digest selector (the column is `NOT NULL DEFAULT ''`; its
selectivity and the index size are the lab measurements that migration needs); (2) the canonical key: `precise_key_ordered_idx` leads with `canonical_key` but is partial on the
precise profile, so an identity lookup by canonical key needs its own `(canonical_key, fact_id)` index (the key is a long concatenation, so its size is the
cost to measure). Each key-first statement then gains an entry in `supportsExplainKeyFences` and a row in `ungrantedExplainDrivingKey`; nothing else moves.

## The plan-shape test (arbiter 7.2) and its seeded red
`explain_key_first_test.go`: `planShapeViolations` parses a plain EXPLAIN and fails a supports node that is a Seq Scan, a probe from the active scopes
(`..._scope_generation_idx`), or a node that carries the route's key only as a Filter (or not at all) instead of as the Index Cond; the operator-override probe
and fact-id lookups are the only other supports nodes allowed.
- **Red (the old statement):** `testdata/explain_item_f_{cve,package}_plain_qa_replica.red.txt` are the real plans the first item f statement got on the QA
  replica (2026-11-24T15:36Z). `TestPlanShapeCheckerRejectsTheScopeDrivenItemFPlan` asserts they fail with "supports probed from the active scopes" and "key only as a
  Filter". The statement and route tests failed to compile before the change (the symbols did not exist).
- **Green (the new statements):** `testdata/explain_item_f_{cve,advisory,package,repository}_{plain,generic}_qa_replica.green.txt` are the supports-node
  projections of the real plain EXPLAIN of each new statement on the QA replica (2026-11-24T16:30Z, custom plan and `plan_cache_mode = force_generic_plan`);
  `TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck` asserts they pass.

## Replica plans (plain EXPLAIN, read-only, replica, 16:30Z)
All 16 plans (8 shapes x custom and generic) are key-first; no Seq Scan on supports, no scope-driven supports probe. Driving node, estimated rows:
cve: Index Scan `cve_idx` 45,265 (custom) / 12,878 (generic); advisory: `advisory_idx` 40,974 + `cve_idx` 7,297 (custom), 12,271 + 12,865 (generic);
package (axios): `package_idx` 2,235,147 (custom) / 48,802 (generic); repository: Bitmap Heap Scan on `repository_idx` 117,810 (custom), Index Scan 22,724 (generic);
the cve+package and the three identity shapes (with a CVE and package selector) drive from `cve_idx` (45,265 / 12,878). Full text: the run directory
`explain-runner/plans/20261124T163016Z/` (`INDEX-plans.txt`, one plan file per shape and variant, `COMMANDS.txt`).

## The two estimate errors behind the old plan (findings, recorded per the ruling)
1. The join of the active scopes (`scope_generations` 15,008 estimated active rows x `ingestion_scopes` 17,105) was estimated at **1 row** against about **15,000**
   real (15,267 active generations on the primary). A join estimate over two correlated columns; column statistics on supports cannot correct it.
2. `pg_stats.n_distinct` for `supports.scope_id` was **441,859** against about **15 to 17 thousand** real scopes, so each scope probe looked like about 26 rows and
   the key filter like 0.03 rows.
No statistics target, no `n_distinct` override and no ANALYZE on QA is the fix (ruling 5.5): the statement must not depend on the estimate.

## Measured work and time (EXPLAIN ANALYZE, replica)
PENDING: run after the coordinator confirms the window state: per selector at three keys (median, 99th percentile, largest by match count from `pg_stats`), twice
under a 25 s statement timeout; the table lands here.

## Not checked
The legacy routes' current plans for the same keys; the live family runs (the coordinator's); that a key's matching supports count equals the supports the driving
node reads when the key has duplicates across scopes (the candidate set is the key's supports, by construction; reported per key in the table above once measured).
