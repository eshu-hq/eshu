# #10880 PR-C1 item f: key-first ungranted explain reads (evidence note)

Branch `drive/10880-prc-c1f`, stacked on C1. Rulings: `.claude/arbiter-c1-item-f-2026-11-24.md`, `arbiter-item-f-time-2026-11-24.md`, `arbiter-item-f-identity-2026-11-24.md` (arbiter, 2026-11-24).

## What this changes
The ungranted explain by selector used to read the whole supports table through a `NOT MATERIALIZED` source joined to the active scopes, with the selector
as an optional predicate (`$4 = '' OR cve_id = $4`). On the QA replica the planner drove that join from the scopes and read every supports row through the
scope index with the selector only as a filter: every other selector and identity shape reached the 25 s statement timeout on run 1 (4 PASS on the two
finishing keys, 10 TIMEOUT of 12 keys; a timed-out shape ran no second run).

Now a call that names a CVE or an advisory drives from that key's own index, and the statement decides none, one or ambiguous before it computes any winner
(`impact/explain_decide_first.go`, section "The decide-first design" below). The driving leg per key, in `explainKeyLegs`:

| key the call names | driving leg | index |
|---|---|---|
| CVE id (`$4`) | `cve_id = $4 AND cve_id <> ''` | `supports_cve_idx` (339) |
| advisory or CVE id (`$3`) | two arms: `advisory_id = $3 AND advisory_id <> ''` UNION ALL `cve_id = $3 AND cve_id <> '' AND advisory_id <> $3` | `supports_advisory_idx` (340), `supports_cve_idx` |

`ungrantedExplainDrivingKey` picks CVE, then advisory-or-CVE. **There is no repository or package statement.** The handler requires a CVE or an advisory unless it is
given a finding id (`HasBoundedScope`, `querycontract/packet/supply_chain_types.go:167-187`), so a repository- or package-driven statement was reachable only as
the identity fallback of a finding id (the store runs the public-id probe first and the selector statement with `$2` set only when the probe misses,
`explain_postgres.go:90-103`). That fallback reads its whole fence even on a hit, and at the largest package it timed out (below), so the ruling removed those two
keys: a finding id with a package, a repository or a subject digest and no CVE or advisory keeps the **legacy** selector read and counts the `legacy` route.
Every other selector, the grant, the identity predicate and the per-candidate authority probe apply to the candidates; the active scopes never drive. The
`<> ''` literal repeats each partial index's predicate so a generic plan can prove the index applies.

## What stays on the legacy read in this PR
A call with a finding id and a package, repository or subject digest and no CVE or advisory, a bare finding id, and a subject digest alone have no supports index to
drive from (the identity fallback reads its whole fence). They keep the legacy selector read. The follow-up PR (arbiter ruling item-f-identity section 3) drives an
identity lookup from the identity: one exact arm per legacy disjunct (fact id by primary key, finding id by `finding_idx`, canonical key, the canonical key of the
authorized fact), then decide-first over that small set; it needs migration 355 (a canonical-key index, three candidate column lists to measure in the lab first),
must not ride C1, and gates only C2's item m together with the subject-digest migration.

Design note, not code, for the two migrations (arbiter 8.4): one `CREATE INDEX CONCURRENTLY` per migration file, each proved in the lab first, then on the replica
by the same plan-shape test: (1) `subject_digest` partial btree `WHERE subject_digest <> ''` for the digest selector (the column is `NOT NULL DEFAULT ''`; its
selectivity and the index size are the lab measurements that migration needs); (2) the canonical key: `precise_key_ordered_idx` leads with `canonical_key` but is partial on the
precise profile, so an identity lookup by canonical key needs its own `(canonical_key, fact_id)` index (the key is a long concatenation, so its size is the
cost to measure). Each statement then gains an entry in `explainKeyLegs`; nothing else moves.

## The plan-shape test (arbiter 7.2) and its seeded red
`explain_key_first_test.go`: `planShapeViolations` parses a plain EXPLAIN and fails a supports node that is a Seq Scan, a probe from the active scopes
(`..._scope_generation_idx`), or a node that carries the route's key only as a Filter (or not at all) instead of as the Index Cond; a primary-key fetch by fact
id and the operator-override probe by canonical key are the only other supports nodes allowed, each only with its own Index Cond.
- **Red (the first item f statement):** `testdata/explain_item_f_cve_plain_qa_replica.red.txt` and
  `testdata/explain_item_f_package_plain_qa_replica.red.txt` are the real plans the first item f statement got on the QA
  replica (2026-11-24T15:36Z). `TestPlanShapeCheckerRejectsTheScopeDrivenItemFPlan` asserts they fail with "supports probed from the active scopes" and "key only as a
  Filter".
- **Green (the decide-first statements):** `go/internal/query/supply/chain/impact/explain_key_first_test.go::TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck`
  runs the 12 supports-node projections of the real plain EXPLAIN of each statement on the QA replica (2026-11-24T18:20Z,
  custom plan and `plan_cache_mode = force_generic_plan`), kept beside the test in its `testdata/` directory; each fixture header carries the sha256 of the
  rendered statement and the test asserts it equals the hash of the statement in the code, so a changed statement fails until it is re-planned on the replica.

## Replica plans (plain EXPLAIN, read-only, replica, 18:20Z)
The 12 plans that remain (cve, advisory, cve+package and the three identity shapes, custom and generic) drive from the CVE or advisory index with an Index Cond. The
package and repository plans of the same run belong to statements this PR removed and are kept in the evidence directory only as history. Driving node, estimated
rows (custom / generic): cve `cve_idx` 45,261 / 1; advisory Bitmap Heap Scan over `advisory_idx` + `cve_idx` / the two arms. Raw files: `10880-prc-c1f-replica/`.

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

1. `key_supports`: a lazy `MATERIALIZED` fence of the support tuples (seven columns, no payload; the planner reports width 114 in the lab and 1,301 on the replica's
   bitmap scans, where the heap fetch carries the whole row) that match the route's own key through its partial index, with
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

`explain_decide_first_live_test.go` runs 172 ungranted requests (126 found, 16 ambiguous, 30 not found; the ambiguous candidate count is compared) through the
decide-first store and the legacy fact-table store on the multi-support corpus plus fixtures: one authorized key beside an unauthorized-only key, a derived-scope
key, a scanner-associated key and a derived-classified key (best-ranked copy of the authorized key stale); two authorized keys with static and retracted
classifications; operator-suppressed keys (a suppressed winner still counts); copies in two scopes; fact-id identity lookups that fall through the public-id
route; runtime filters (service and workload facts naming one of two repositories); subject digests; an advisory id carried as `advisory_id` and in the `cve_id`
column; rank ties between authorized copies (a copy without a payload finding id whose fact id sorts first, and equal copies decided by fact id); and a finding
id with a package, a repository or a digest and no CVE or advisory, hit by fact id, hit by canonical key and missed (these run the legacy read in both stores).
Every request equals legacy. Nine seeded violations each make the same matrix fail: no active-generation check, no scanner-association guard, no second-key
compare, no runtime-filter predicate, no subject-digest predicate, ranked copies in reverse, no payload-finding-id tie-break, fact id tie-break descending, no
identity predicate. `TestExplainSelectorJITOffRevertsAfterTheExplainLive` shows the jit setting reverts after the explain on the plain connection and the guarded
read port, for a found, an ambiguous and a not-found answer.

## Lab work and time (disposable `f_scale` database, 450,300 supports, run 2, ms)

2,000 active scopes, 10% of copies in a superseded generation. Hot CVE 140,000 copies over 679 keys; 40,000 over 200; one key with 30,000 copies.
(The package- and repository-driven lab rows of the earlier version belong to statements this PR removed.) `key_supports` rows read and authority-probe loops (from EXPLAIN ANALYZE):

| key | plain / generic ms | rows read | authority probes | answer |
|---|---|---|---|---|
| cve, 140,000 copies | 120 (107 of it JIT) / 0.96 | 2 / 7 | 2 / 7 | ambiguous |
| cve, 40,000 copies | 4.6 / 1.1 | 2 / 7 | 2 / 6 | ambiguous |
| advisory, 140,000 | 13.8 / 0.95 | 2 / 7 | 2 / 7 | ambiguous |
| cve, ONE key with 30,000 copies | 83 / 86 | 30,000 | 2 | one |
| cve, no match | 0.6 | 0 | 0 | none |

Before the ranked-copy step the one-key case took 410 ms and 30,001 probes. The lab chooses a Seq Scan for the 31%-selectivity hot CVE, so plan shape rule 7.2 is
judged on the replica below; the 107 ms of JIT in the first row is the cost the `jit = off` switch removes (section "JIT").

## Replica results (QA replica, 2026-11-24)

READ ONLY, 25 s statement timeout, custom plan unless stated, replay lag 0.0 to 0.3 s. Plain EXPLAIN 18:20:00Z to 18:20:14Z; EXPLAIN (ANALYZE, BUFFERS) twice at the
baseline keys 18:20:23Z to 18:20:57Z; single-key timings 18:21:33Z to 18:21:59Z; request shapes 18:37:08Z to 18:38:56Z; identity ruling run 19:16:27Z to 19:17:41Z.
Raw files: `10880-prc-c1f-replica/`.

### CVE and advisory keys with an empty identity (the ambiguous answer)
Decide-first against legacy, ms (run 1 / run 2), rows read from the key fence and authority-probe loops:

| selector | class | key | matching supports | decide-first r1 / r2 ms | legacy r1 / r2 ms | key rows read | authority probes |
|---|---|---|---|---|---|---|---|
| cve | median | CVE-2021-26539 | 13,437 | 34 / 30 | 3,816 / 2,963 | 2 | 2 |
| advisory | median | CVE-2026-73566 | 8,384 | 33 / 30 | 1,776 / 1,761 | 2 | 2 |
| cve | p99 | CVE-2026-102277 | 86,504 | 42 / 33 | TIMEOUT | 2 | 2 |
| advisory | p99 | CVE-2026-14257 | 86,497 | 1,343 / 111 | TIMEOUT | 7 | 12 |
| cve | largest | CVE-2022-3517 | 140,256 | 88 / 85 | TIMEOUT | 15 | 28 |
| advisory | largest | GHSA-f8q6-p94x-37v3 | 140,256 | 89 / 88 | TIMEOUT | 15 | 28 |

The package and repository rows of the same twelve-key run (median, p99, largest; 29 to 84 ms second runs; legacy TIMEOUT) timed a call the handler refuses
(`$2 = ''` with no CVE or advisory): **not reachable, no claim is made on them**; their raw plans stay in the evidence directory. Every CVE and advisory second run
above is under 112 ms against the 1,500 ms bar; the 1,343 ms first run is a cold read of 7 fence rows and 12 probes.

### One finding, none, and the largest case measured (CVE-2022-3517, 140,256 rows)
| shape | custom plan r1 / r2 ms | generic plan r1 / r2 ms | fence rows read | probes | answer |
|---|---|---|---|---|---|
| CVE + minimatch + repository r_0c0bb73f (one canonical key) | 111 / 73 | 708 / 693 | 209 | 2 | one |
| CVE + an absent package | 32 / 30 | 725 / 689 | 0 | 0 | none |

CVE plus package alone leaves several canonical keys under every package of that CVE (the key carries the version and the repository), so one key needs the repository
too. Most of the generic-plan time is execution (about 0.65 s); planning the generic plan is about 0.04 s.

### Identity fallback with a CVE or an advisory (ruling 2.2)
The fence is read to its end because the identity predicate cannot end the scan; time is about 7.2 microseconds per fence row (temp blocks of 8 kB):

| selector and identity | r1 / r2 ms | temp blocks read / written (run 2) | probes | answer |
|---|---|---|---|---|
| CVE-2022-3517 (140,256 rows), finding id that misses | 1,123 / 1,033 | 49,292 / 24,646 | 0 | none |
| CVE-2022-3517, fact id of one copy (hit) | 1,032 / 1,010 | 24,646 / 24,646 | 3 | one |
| GHSA-f8q6-p94x-37v3 (140,256 rows), finding id that misses | 1,612 / 1,227 | 49,292 / 24,646 | 0 | none |
| GHSA-f8q6-p94x-37v3, fact id of one copy (hit) | 1,055 / 1,070 | 24,646 / 24,646 | 3 | one |

Pass (second run at or under 1,500 ms): a finding id with a CVE or an advisory stays on decide-first. **This is the largest case measured; the time grows with the
fence**, so a CVE or advisory with a larger fence would cost more: a fence above about 190,000 rows is over the bar by the same arithmetic (not measured).

### Identity fallback with no CVE and no advisory stays on legacy
A finding id with a package, a repository or a subject digest and no CVE or advisory keeps the legacy selector read. The decide-first package statement at axios
(2,232,312 rows; statement sha256 `60fe5445b3e8a1146aceb86c9ea4ed31930605bcc38dbb416fe0aa721d57f8f7`) timed out at 25 s on run 1 (no plan captured), costing about
7.9 microseconds and 1.3 kB of temp per fence row. The legacy selector read for the same calls, identity miss + axios and identity hit (fact id) + axios, also times
out at 25 s on run 1 (run 2 not run). **This PR does not improve that shape and does not claim to**; there is no caller data on how often it is sent, so it is not
called rare. The identity-first follow-up PR is the fix.

### The key with the most copies per canonical key
`pg_stats` top `canonical_key` (about 2,733 copies, `CVE-2026-8723` with `npm://registry.npmjs.org/qs`): the CVE and package selector is ambiguous (7 rows read,
12 probes; 133 ms in the ANALYZE index run, 100 ms in the run-2 plan). A fact-id identity lookup on a copy of that key with the same CVE and package scans past
56,269 fence rows the identity filter removes, then ranks the key's copies: 483 ms in the run-2 plan. That was measured before the ruling and is the same fence-scan
case as the identity table above.

## JIT
The replica has `jit = on`, `jit_above_cost = 100000`, `jit_inline_above_cost = 500000`, `jit_optimize_above_cost = 500000` (server 18.6). The top plan cost at the largest
keys is above `jit_above_cost`, and no EXPLAIN ANALYZE of this statement on the replica shows a JIT section (`pg_jit_available()` was not read). The lab, with LLVM,
does JIT, and the cost is large (disposable `f_scale`, 2,950,300 supports, `jit_above_cost` 100,000, JIT block recorded; run 2, ms of execution, of which JIT):

| key | plan | top plan cost | jit on | jit off |
|---|---|---|---|---|
| CVE, 1.6 million copies | custom | 831,151 (above the 500,000 inline and optimize thresholds) | 1,421 (JIT 1,406, 224 functions) | 0.57 |
| CVE, 900,000 copies | custom | 383,447 | 119 (JIT 105) | 0.58 |
| advisory, 900,000 copies | custom | 383,133 | 118 (JIT 107) | 0.60 |
| CVE, 140,000 copies | custom | 289,832 | 127 (JIT 106) | 11.5 |
| any of the above | generic | 1,363 to 12,451 | no JIT | no JIT (0.8 to 1.0) |

At 1.6 million copies JIT alone is a 1,421 ms statement against the 1,500 ms bar; off is 2,500 times faster. The statement's designed work is 2 to 30 rows and its
plan cost is an estimate over branches that never run, so a server with LLVM (this is a self-hosted product) must not pay the compile. The decide-first selector read
now runs `SELECT set_config('jit', 'off', true)` inside the explain snapshot transaction already open, immediately before the `supports_selector` statement
(`explain_postgres.go`); no second transaction, the other explain statements are unchanged, and the setting is gone at the snapshot's commit or rollback. The pair
was run through psql with `PGOPTIONS='-c jit=on|off'` on the same prepared statement; the Go store's end-to-end behavior is the live revert test and the hermetic order test
(`TestExplainSwitchesJITOffOnlyBeforeTheKeyedSelectorRead`). Post-roll record, not a gate: read-only `SHOW jit` and `SELECT pg_jit_available()` on the QA primary and the replica.

## Probe cost
`second_hit` re-probes a row `first_hit` already rejected, so a candidate whose authority probe fails is probed up to twice. The authority-probe figures above are
therefore bounded by twice the rows read; they were measured, not tuned.

## Claims table

Role: the replica rows use the read-only login on the replica; the live rows use the owning role of a disposable PostgreSQL 18 database in the lab. Raw replica
and lab evidence is committed under `10880-prc-c1f-replica/`.

| Claim | Test or run | Seeded violation that fails the NAMED test or run | Role | N |
|---|---|---|---|---|
| A CVE or advisory call with an empty identity meets 1,500 ms (second run) at median, p99 and largest | `decide-first-analyze-INDEX.txt` (CVE and advisory rows) | `baseline-legacy-INDEX.txt`: legacy, same keys and limits, 10 of 12 keys TIMEOUT | QA replica, read only | 6 keys x 2 runs |
| Package and repository selectors | **not reachable**: `HasBoundedScope` refuses a call with no finding id and no CVE or advisory; no claim | `TestUngrantedExplainStatementsDriveFromTheirKey` fails if a `repository` or `package` leg returns | hermetic | 2 keys removed |
| One and none answers meet the bar (custom and generic plans) | `p21-results.txt`, `p22-results.txt` (111 / 73 and 32 / 30 ms custom; 708 / 693 and 725 / 689 ms generic) | the unranked version probed 30,001 candidates (lab, 410 ms) | QA replica; lab | 4 shapes x 2 runs |
| Identity fallback with a CVE or an advisory meets the bar at the largest fence (140,256 rows) | `p22-results.txt`: second runs 1,010 to 1,227 ms | the same shape at the package statement timed out at 25 s (`p21b-identity-fallback-package.r1.txt`); a fence above about 190,000 rows would be over the bar (not measured) | QA replica | 4 shapes x 2 runs |
| **Identity fallback with no CVE and no advisory: stays on legacy, not improved, not claimed** | `p22-results.txt`: legacy miss + axios and hit + axios both TIMEOUT at 25 s | n/a (follow-up PR, migration 355) | QA replica | 2 shapes x 1 run |
| Finding id with a package, repository or digest and no CVE or advisory runs the legacy statement and counts `legacy` | `TestUngrantedExplainRoutePicksTheDrivingKey`, `TestExplainCountsEachStatementByRoute` | restoring a `PackageID` case in `ungrantedExplainDrivingKey` FAILS the route test (finding id and a package, package alone); the clean run passes (`route-and-jit-mutation-proof.txt`; the earlier four-key `route-mutation-proof.txt` is kept as history) | hermetic | 10 legacy filters, 12 count cases |
| The keyed selector read runs under a transaction-local `jit = off`, right before the statement and nowhere else | `TestExplainSwitchesJITOffOnlyBeforeTheKeyedSelectorRead`; live `TestExplainSelectorJITOffRevertsAfterTheExplainLive` | neutering the `if route == RouteSupportsSelector && s.inSnapshot` guard in `loadExplanationFindingsByRoute` FAILS the order test on all three keyed subtests; the clean run passes (`route-and-jit-mutation-proof.txt`) | hermetic; disposable PostgreSQL | 9 filters; 2 stores x 3 answers |
| JIT off is faster where JIT triggers | `lab-jit-pair-1.6M-copies.txt`, `lab-jit-pair-900k-and-hot.txt` | jit on: 1,421 ms at 831,151 plan cost vs 0.57 ms off | lab, PostgreSQL with LLVM | 4 keys x 2 plans x 3 runs |
| Plan shape 7.2 under custom and generic plans for the CVE, advisory, cve+package and identity shapes | `TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck` over 12 replica fixtures, each stamped with the statement hash | `TestPlanShapeCheckerRejectsTheScopeDrivenItemFPlan` (real scope-driven plans), `TestPlanShapeCheckerRejectsAFullPrimaryKeyOrOperatorScan`; a changed statement fails the hash stamp | QA replica plans | 12 plans, 6 shapes |
| The statement streams (lazy fence, no blocking node, cheap compare before the probe, LIMITed lateral probes, runtime filter kept) | `TestUngrantedExplainStatementsDriveFromTheirKey` | `TestExplainStatementShapeCheckRejectsSeededViolations`: a sort in the fence, no cheap compare, a plain scope join, the payload in the fence, the runtime filter dropped | hermetic | 2 statements, 5 mutations |
| Answer equals legacy over zero, one and several keys, unauthorized-only and stale best copies, scanner association and classification, derived scope, operator suppression, identity lookups, runtime filters, subject digest, advisory ids, rank ties, the ambiguous count, and a finding id with a package, repository or digest | `TestExplainUngrantedDecideFirstEqualsLegacyLive` ("equals legacy") | nine subtests, each making the same matrix fail (listed above) | disposable PostgreSQL, owner | 172 requests (126 found, 16 ambiguous, 30 not found) |
| Rows read and authority probes are bounded for an ambiguous answer whatever the match count | replica table above; lab `f_scale` table | first key-first statement: about 70 microseconds per candidate on 140,292 candidates | QA replica; lab | 6 keys; 5 lab keys |

## Follow-up and the item m block
The identity-first statement (arbiter ruling item-f-identity section 3) is a separate PR with migration 355 (a canonical-key index: measure `(canonical_key)`,
`(canonical_key, fact_id)` and the column list of 343 without its predicate in the lab first), not on C1; it gates only C2's item m together with the subject-digest
migration. The follow-up is #11370 (identity-first plus migration 355); the residual comment on #10880 names it, migration 355 and the item m block.

## Not checked
The legacy routes' current plans for the same keys; the live family runs (the coordinator's); that a key's matching supports count equals the supports the driving
node reads when the key has duplicates across scopes (the candidate set is the key's supports, by construction; reported per key in the table above once measured).
