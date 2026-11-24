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

## The legacy baseline (what the two explain routes ran before this PR)

Arbiter ruling `item-f-time` (2026-11-24) asked for the legacy statement at the same twelve keys under the same limits first. QA replica
(recovery=t), READ ONLY, 25 s statement timeout, custom plan, run 17:25:07Z to 17:38:56Z, replay lag 0.0 to 0.2 s throughout; run directory
`explain-runner/plans/baseline-20261124T172505Z/`. Run 1 / run 2 in ms:

| selector | key class | matching supports | legacy r1 / r2 |
|---|---|---|---|
| cve | median | 13,437 | 3,816 / 2,963 (answer: ambiguous, 2 rows) |
| advisory | median | 8,384 | 1,776 / 1,761 (answer: ambiguous, 2 rows) |
| repository | median | 12,854 | TIMEOUT |
| package | median (defu) | 1,320 | TIMEOUT |
| cve | p99 | 86,504 | TIMEOUT |
| advisory | p99 | 86,497 | TIMEOUT |
| repository | p99 | 125,009 | TIMEOUT |
| package | p99 (minimatch) | 841,668 | TIMEOUT |
| cve | largest | 140,277 | TIMEOUT |
| advisory | largest | 140,277 | TIMEOUT |
| repository | largest | 168,646 | TIMEOUT |
| package | largest (axios) | 2,232,312 | TIMEOUT |

Legacy meets 1,500 ms at none of the twelve keys; ten do not finish in 25 s. The replica refuses `md5` ("could not compute MD5 hash: unsupported"), so
the answer class is a sha256 of the returned rows. The two keys that finish are ambiguous (ids and hashes in the run directory).

## Why the first key-first statement was not enough, and the decide-first design

The first key-first statement (previous commit) drove every selector from its own index and removed the scope-driven plan, but still computed every
candidate's winner: on a CVE with 140,292 candidates and 679 canonical keys it spent about 70 microseconds per candidate on the scope, generation and
classification probes and sorted every candidate with its payload. Its replica ANALYZE misses the 1,500 ms bar at the 99th percentile and the
largest keys (`analyze-20261124T170009Z`). The route answers at most one finding and a selector that matches two canonical keys is a refusal that
carries only the count (`ErrExplanationAmbiguous`, `explain_postgres.go`), so the winners of a broad selector were computed to be thrown away.

`explain_decide_first.go` decides "none, one or ambiguous" first:

1. `key_supports`: a lazy `MATERIALIZED` fence of the narrow support tuples (no payload) that match the route's own key through its partial index, with
   every selector, grant, runtime-filter and column-only authority predicate. No sort, aggregate or distinct inside it, so a LIMIT above stops the scan.
2. `first_hit` and `second_hit`: `LIMIT 1` reads of the fence. The second compares `canonical_key` with the first's BEFORE the authority probe, so the
   probe runs only on a candidate of a canonical key not yet found. The probe is a `LATERAL` subquery with a `LIMIT`, which the planner cannot pull up into
   a join it orders from a row estimate (both estimate errors below): it runs per candidate that reaches it, as a nested loop, whatever the estimates say.
3. Two canonical keys = ambiguous: the two hit rows are returned with their payload fetched by primary key. No winner is computed; the store counts rows
   and the envelope carries the same count as legacy's (2).
4. Exactly one key (the scan ran to its end): its copies are ranked as the winner CTEs rank them and probed in that order to the first authorized copy.
   The winner CTEs keep only the best-ranked authorized copy of a key, so no lower-ranked copy can change the answer; the unchanged winner CTEs run on
   that one row, its payload fetched by primary key. A key whose every copy fails the probe contributes nothing; a key whose best copy fails it is
   answered by the next authorized copy.

## Differential and seeded violations (disposable PostgreSQL, lab)

`explain_decide_first_live_test.go` runs 91 ungranted requests (73 found, 7 ambiguous, 11 not found; the ambiguous candidate count is compared)
through the decide-first store and the legacy fact-table store on the multi-support corpus plus fixtures: one authorized key beside an
unauthorized-only key, a derived-scope key, a scanner-associated key and a derived-classified key (best-ranked copy of the authorized key stale);
two authorized keys with static and retracted classifications; operator-suppressed keys (a suppressed winner still counts); copies in two scopes;
fact-id identity lookups that fall through the public-id route. Every request equals legacy. Four seeded violations each make the same matrix fail:
no active-generation check, no scanner-association guard, no second-key compare, no identity predicate.

## Lab work and time (disposable `f_scale` database, 450,300 supports, run 2, ms)

2,000 active scopes, 10% of copies in a superseded generation. Hot CVE 140,000 copies over 679 keys; 40,000 over 200; one key with 30,000 copies;
an axios-like package with 150,000 copies over 5,000 keys. `key_supports` rows read and authority-probe loops (from EXPLAIN ANALYZE):

| key | plain / generic ms | rows read | authority probes | answer |
|---|---|---|---|---|
| cve, 140,000 copies | 120 (107 of it JIT) / 0.96 | 2 / 7 | 2 / 7 | ambiguous |
| cve, 40,000 copies | 4.6 / 1.1 | 2 / 7 | 2 / 6 | ambiguous |
| advisory, 140,000 | 13.8 / 0.95 | 2 / 7 | 2 / 7 | ambiguous |
| package, 150,000 (axios-like) | 117 (106 of it JIT) / 0.9 | 2 / 8 | 2 / 7 | ambiguous |
| repository, about 1,100 | 0.8 / 1.1 | 2 / 2 | 2 / 2 | ambiguous |
| cve, ONE key with 30,000 copies | 83 / 86 | 30,000 | 2 | one |
| cve, no match | 0.6 | 0 | 0 | none |

Before the ranked-copy step the one-key case took 410 ms and 30,001 probes. The lab chooses a Seq Scan for the 31%-selectivity hot CVE and the two
plain runs above include about 106 ms of JIT (plan cost over the JIT threshold from unexecuted branches); neither applies to the replica (key shares are
tiny there; its earlier plans show no JIT), so plan shape rule 7.2 is judged on the replica below.

## Replica plans and time (QA replica, 2026-11-24)

QA replica (recovery=t), READ ONLY, 25 s statement timeout, custom plan, replay lag 0.0 to 0.3 s throughout, ports closed after. Plain EXPLAIN of the
eight shapes under custom and force_generic_plan 18:20:00Z to 18:20:14Z (`explain-runner/plans/20261124T182000Z/`); EXPLAIN (ANALYZE, BUFFERS) twice at
the twelve baseline keys 18:20:23Z to 18:20:57Z (`plans/analyze-f2-20261124T182022Z/`).

Plan shape (arbiter 7.2): all sixteen plans drive the supports read from the index on the route's own key with an Index Cond (`cve_idx`, `advisory_idx` plus
`cve_idx`, `package_idx`, `repository_idx` or its bitmap scan); no Seq Scan on supports; no `scope_generation_idx` probe; the other supports nodes are
primary-key fetches (`support`, `support_1`: the two hit rows and the best copy) and the operator-override probe. The authority probe reads
`ingestion_scopes`, `scope_generations` and the two scanner tables, one candidate at a time (nested loop under a `Limit`). The eight custom and eight
generic projections are the checked-in green fixtures (`testdata/explain_item_f_*_qa_replica.green.txt`), replacing the previous statement's.

Decide-first against legacy, ms (run 1 / run 2), rows read from the key fence and authority-probe loops from EXPLAIN ANALYZE:

| selector | class | key | matching supports | decide-first r1 / r2 ms | legacy r1 / r2 ms | key rows read | authority probes |
|---|---|---|---|---|---|---|---|
| cve | median | CVE-2021-26539 | 13437 | 34 / 30 | 3,816 / 2,963 | 2 | 2 |
| advisory | median | CVE-2026-73566 | 8384 | 33 / 30 | 1,776 / 1,761 | 2 | 2 |
| repository | median | repository:r_4419d5a6 | 12854 | 34 / 29 | TIMEOUT | 2 | 2 |
| package | median | npm://registry.npmjs.org/defu | 1320 | 34 / 30 | TIMEOUT | 2 | 2 |
| cve | p99 | CVE-2026-102277 | 86504 | 42 / 33 | TIMEOUT | 2 | 2 |
| advisory | p99 | CVE-2026-14257 | 86497 | 1343 / 111 | TIMEOUT | 7 | 12 |
| repository | p99 | repository:r_c0174eba | 125009 | 79 / 73 | TIMEOUT | 2 | 2 |
| package | p99 | npm://registry.npmjs.org/minimatch | 841542 | 32 / 30 | TIMEOUT | 2 | 2 |
| cve | largest | CVE-2022-3517 | 140256 | 88 / 85 | TIMEOUT | 15 | 28 |
| advisory | largest | GHSA-f8q6-p94x-37v3 | 140256 | 89 / 88 | TIMEOUT | 15 | 28 |
| repository | largest | repository:r_acfb2844 | 168646 | 88 / 84 | TIMEOUT | 2 | 2 |
| package | largest | npm://registry.npmjs.org/axios | 2232312 | 38 / 36 | TIMEOUT | 2 | 2 |

Every second run is under 112 ms (the bar is 1,500 ms); every first run is under 1,350 ms. The 1,343 ms first run (advisory 99th percentile) is a cold
read of 7 fence rows and 12 probes. Each answer is ambiguous (2 rows) except where noted. Rows read stay at or below the matching supports and, for an
ambiguous answer, at 2 to 15 whatever the match count (2,232,312 for axios); probes at 2 to 28.

The key with the most copies per canonical key (top most-common `canonical_key` in `pg_stats`: about 2,733 copies, `CVE-2026-8723` with `npm://registry.npmjs.org/qs`):
the CVE and package selector is ambiguous (7 rows read, 12 probes; 644 / 100 ms). The case that stays proportional is a selector that decides on exactly one
canonical key, because the scan must then reach its end: a fact-id identity lookup on a copy of that key with the same CVE and package reads all 56,269
matching supports (narrow tuples), ranks the key's copies and probes 3 of them: 586 / 513 ms. That is the worst measured case and is under the bar.

## Claims table

Role: the QA replica rows use the read-only login on the replica; the live rows use the owning role of a disposable PostgreSQL 18 database in the lab.

| Claim | Test or run | Seeded violation (RED) | Role | N |
|---|---|---|---|---|
| Ungranted selector explain meets 1,500 ms (second run) at median, p99 and largest for cve, advisory, package, repository | `analyze_f2.py` run above (not a repo test) | legacy baseline: 10 of 12 keys TIMEOUT, the other two 1.8 to 3.8 s | QA replica, read only | 12 keys, 2 runs each |
| Plan shape 7.2 holds under custom and generic plans | `TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck` over the replica plan fixtures | `TestPlanShapeCheckerRejectsTheScopeDrivenItemFPlan` over the previous statement's real replica plans | QA replica plans | 8 plans |
| Statement drives from the key, streams without a blocking node, probes only after the cheap key compare | `TestUngrantedExplainStatementsDriveFromTheirKey` | the live seeded violation "no second-key compare" below | hermetic | 4 statements |
| Route picks the driving key and keeps the legacy read for a lookup without an indexed key | `TestUngrantedExplainRoutePicksTheDrivingKey`, `TestExplainCountsEachStatementByRoute` rows | none beyond the route assertions themselves (a wrong route fails them) | hermetic | routes per driving key and the keyless fallback |
| Answer equals legacy over zero, one and several keys, an unauthorized-only key, a best copy that is unauthorized, scanner association and classification, a derived scope, operator suppression, identity lookups, and the ambiguous count | `TestExplainUngrantedDecideFirstEqualsLegacyLive` ("equals legacy") | subtests: no active-generation check, no association guard, no second-key compare, no identity predicate each fail the matrix | disposable PostgreSQL, owner | 91 requests (73 found, 7 ambiguous, 11 not found) |
| Rows read and authority probes are bounded for an ambiguous answer, whatever the match count | replica table above; lab `f_scale` table | first key-first statement: about 70 microseconds per candidate on 140,292 candidates | QA replica; lab | 12 keys; 9 lab keys |
| The one-key case (scan to its end) stays under the bar | replica fact-id identity timing above | the unranked version probed 30,001 candidates in the lab (410 ms) | QA replica; lab | 1 key (56,269 fence rows) |

## Not checked
The legacy routes' current plans for the same keys; the live family runs (the coordinator's); that a key's matching supports count equals the supports the driving
node reads when the key has duplicates across scopes (the candidate set is the key's supports, by construction; reported per key in the table above once measured).
