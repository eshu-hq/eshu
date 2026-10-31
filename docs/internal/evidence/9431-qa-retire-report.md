# #9431 QA retire — evidence record

One-time operational retire of stale OSV generations on the QA environment
(QA cluster, namespace `eshu`), per
[9431-arbiter-ruling.md](9431-arbiter-ruling.md). No Go code change.
Script: [9431-qa-retire.sql](9431-qa-retire.sql).
Class W list: [9431-class-w-advisories.txt](9431-class-w-advisories.txt).

Lane branch `drive/9431-retire-stale-osv`, rebased to `origin/main`
`761f65e2f7` before promotion (original commit on `ca2fcbe4ab`). All QA access is read-only psql via
`kubectl exec -n eshu eshu-postgresql-0` except the single authorized
Section 4 transaction (not yet run).

## Step 1 — satellite precondition (PROVEN 2026-10-30 ~02:30 UTC)

- Deployed QA satellite: the satellite `Deployment` (QA), image
  `ghcr.io/eshu-hq/eshu:sha-05517e5978a56cdd2fc502c1f08806540c1915a8`,
  created 2026-10-27T23:06:18Z. Schedule `...:schedule:derived:...` runs
  complete every ~30 min (latest `scheduled-20261030T013000Z`).
- Live #9421 proof: GHSA-6w3j-5fw6-r9vr, package `joi`. OSV truth carries
  two `affected[]` blocks for `joi` (`[17.2.0, fixed 17.13.6]` and
  `[18.0.0, fixed 18.2.5]`). The live satellite fact
  (the satellite instance) carries both ranges in `affected_ranges` and
  both fixed versions `["17.13.6", "18.2.5"]` — every entry, not last-wins.
  The old in-tree shape keeps only the last envelope per fact id
  (`facts.go:355-375`). Gate passes; retiring does not recreate the bug.
- NOT_VERIFIED: image-digest-to-#9421-fix linkage beyond this behavior.

## Step 2 — candidate set (measured 2026-10-30 ~02:10–02:25 UTC)

Predicate (script Section 1): active generation
(`scope.active_generation_id = g.generation_id`, `g.status = 'active'`),
`scope_id LIKE 'vuln-intel://osv/%'`, a live `vulnerability.affected_package`
fact stamped with the retired instance, no live fact in the
generation stamped any other instance, no `fact_work_items` in
`claimed/running/retrying`.

- Candidates: **136** generations (136 scopes, one generation each).
- Class W: **72**. Class S: **64** (12 fully satellite-covered, 52 remainder).
- Mixed-instance: **0**. In-flight: **0**.
- Class W list provenance: 523 distinct GHSA ids in candidates fetched from
  `api.osv.dev/v1/vulns/<id>` 2026-10-30 (0 failures); **229** carry one
  package name in 2+ `affected[]` blocks. 229/576 distinct candidate
  advisories = 39.8% — exactly the issue body's 229 advisories / 39.8%.
- Satellite coverage: 263 advisories live (419 facts, 60 scopes); 31 of the
  229 Class W advisories already have a satellite replacement.
- Class S remainder: 52 scopes, 93 advisories without satellite replacement —
  stays active, follow-up re-collection issue to be filed with the PR.
- GHSA-67hx-6x53-jw92: only retired-instance facts live (2 scopes); satellite
  has not recollected it yet — owned by step 4. Pre-fix symptom confirmed
  live: `reducer_supply_chain_impact_finding` for `@babel/traverse@7.29.7`
  reads `possibly_affected / version_not_in_advisory_range /
  >=8.0.0-alpha.0 <8.0.0-alpha.4` (2 findings).

## Step 3 — dry run (2026-10-30, `9431-qa-retire.sql` Sections 0–3, exit 0)

- `class_w_ids = 229`; classification `136 / 72 / 12 / 0 / 0` matches step 2.
- Retire set: **84** generations (72 W + 12 S-eligible), 0 blockers.
- `BEGIN ... ROLLBACK`: lock 84 rows in `(scope_id, generation_id)` order,
  in-flight recheck 0, update 84 generations + clear 84 scope pointers,
  counts reconcile (`dryrun_updated_generations=84`,
  `dryrun_cleared_scopes=84`), rolled back.
- Post-dry-run: 0 rows carry the `retired_issue=9431` marker, 0 OSV scopes
  with NULL pointer. No data changed.
- Re-validation after parameterizing instance ids as psql variables (same
  script, `-v retired_instance/satellite_instance`, exit 0): classification
  moved to 136 / 69 / 12 / 0 / **3**, retire set 81 — three Class W
  generations carry seconds-old `claimed` reducer rows from the normal
  impact cycle and are excluded fail-closed. In-flight membership
  fluctuates minute to minute; the script recomputes the set at run time
  and re-checks inside the transaction, so this is the mechanism working,
  not a blocker.
- Lock + UPDATE statements byte-identical between Sections 3 and 4
  (diff of the two blocks; only reporting tail and COMMIT vs ROLLBACK differ).
- Scope status `failed` + NULL pointer: the existing no-active-generation
  value (`failProjectorWorkQuery`, `projector_queue_sql.go:239-254`).
  Consumers: matcher ignores scope status
  (`facts/impact/branches.go:77-91`); recovery reports this shape as
  `no_recoverable_generation`, "only a new collection heals it"
  (`7116-recover-failed-scopes.md:50-57`); scope upsert heals both columns
  (`ingestion_queries.go:108-118`); `ingestion_scopes.status` is overwritten
  on every upsert so it carries no durable marker (7766 design) — the durable
  marker is the generation payload (`retired_issue: 9431`).
- Baseline drift vs 2026-10-29 lane (574 gens, 1115 facts): RECONCILED EXACTLY.
  Re-ran the lane predicate without the `affected_package` restriction:
  **574** active OSV generations carry a live retired-instance fact of any
  kind. The 136 retire candidates are the subset carrying retired
  `affected_package` facts (the verdict's predicate); the other 438 hold
  only advisory-metadata facts (e.g. `vulnerability.cve`), which the matcher
  never reads and the verdict does not retire. Affected-package facts
  reconcile exactly (**1115** then and now; satellite 419 now vs 389 then).
  Class W generation count is stable (72 then, 72 now).
  Non-OSV retired-instance facts (427,317 live, of which 418,169 are
  `vuln-intel://cisa/kev`, a scope the satellite recollects every run as a
  configured target) are outside the verdict's OSV predicate.

## Step 4 — replacement pass (BLOCKED on rotation; ETA ~91 h from 2026-10-30 ~02:45 UTC)

`@babel/traverse@7.29.7` derivation input is live: 52 lockfile facts
(`entity_metadata.value = 7.29.7`, pnpm) in active generations across 50
repository scopes, read by `ListOwnedPackageDependencyTargets`
(`owned_package_targets.go:133`). The satellite plans rotating
30-min windows (`schedule:derived:scheduled-<ts>`, 2–5 scopes/run, all
complete, 0 open). Rotation math, all values measured: T = 31,875 distinct
owned targets, L = 100 (default, unset in activation) → P = 1024 slots;
traverse key `npm|@babel/traverse|7.29.7` hashes to slot **124**; current
slot 966 → **182 windows ≈ 91 h** until its window (slot math in
`owned_package_targets.go:192-198`, `schedule/derivation.go:100-120`).
No intervention needed or authorized — awaiting the natural cycle, then
collection + projection + reducer impact flip for GHSA-67hx-6x53-jw92.

Watch (read-only, one indexed query):
`SELECT count(*) FROM fact_records WHERE fact_kind =
'vulnerability.affected_package' AND is_tombstone = false AND
payload->>'collector_instance_id' = <satellite QA instance, see #9431> AND
payload->>'advisory_id' = 'GHSA-67hx-6x53-jw92';`
— proceed to Section 4 when it returns ≥1 AND the fact carries both release
lines (AC3.3a shape). Re-run Sections 0–3 first: the retire set is
recomputed at run time (satellite coverage only grows).

Question for the coordinator/arbiter (no reinterpretation made here): if the
wait becomes a problem, 31 Class W advisories already have live satellite
replacements (e.g. GHSA-6w3j-5fw6-r9vr/`joi`, both entries proven live),
which could carry the same end-to-end proof. The ruling names GHSA-67hx;
that substitution needs a recorded ruling, not lane discretion.

## Step 4 — update 2026-10-31 14:48 UTC (collection PROVEN, impact flip pending retire)

Watch fired on the read replica (`eshu-postgresql-read-0`, read-only
transactions, 15–60s `statement_timeout`, all exits 0). Note: the
`eshu-postgresql-read` ClusterIP timed out on every attempt (even `SELECT
1`), so reads went to the replica pod IP directly; still replica-only, no
primary reads except the authorized dry run below.

- AC3.3a shape PROVEN live: 2 satellite live `affected_package` facts for
  GHSA-67hx-6x53-jw92 in the active generation. The `@babel/traverse` row
  carries both release lines (`[{"introduced":"0"},{"fixed":"7.23.2"}]`
  and `[{"introduced":"8.0.0-alpha.0"},{"fixed":"8.0.0-alpha.4"}]`) and
  both fixed versions `["7.23.2","8.0.0-alpha.4"]` — every entry, not
  last-wins. (Second row is the unscoped `babel-traverse` package name in
  the same satellite scope.)
- Retired-instance GHSA-67hx facts still live (4) — expected pre-retire.
- Impact for the 7.29.7 pin still `possibly_affected` ×3 (one new
  `>=0 <7.23.2` row beside the two `>=8.0.0-alpha.0 <8.0.0-alpha.4`
  rows). The flip to `affected` is expected only after the retire commit
  (ruling step 6), so this is the pre-retire baseline, not a regression.
- `_slot` scopes on QA: **0**. Slot scopes (#9587) are not enabled on the
  QA satellite; collection still runs legacy batch scopes, so the V/P
  rotation math is unchanged — but the ~91 h ETA is moot for GHSA-67hx:
  the natural cycle already delivered the replacement.
- Dry run re-run (committed hardened script, primary, Sections 0–3 + 6,
  exit 0, Section 4 SKIPPED without `retire_ok`, rolled back):
  classification `136 / 67 / 18 / 0 / 5` vs the 2026-10-30 `136 / 72 /
  12 / 0 / 0`. Drift reconciled: 5 Class W generations carry
  seconds-old `claimed` reducer rows and are excluded fail-closed (same
  mechanism as the earlier 72→69 move); Class S eligible grew 12→18 as
  satellite coverage grew (AC3.4 live satellite facts 419→795).
  Retire set **85** (67 W + 18 S), `must_be_zero_blockers=5` (the 5
  in-flight, excluded). `dryrun_updated_generations=85`,
  `dryrun_marker_prior_status=85`, `dryrun_cleared_scopes=85`.
- Post-run: 0 rows carry the `retired_issue=9431` marker, 0 OSV scopes
  with NULL pointer. No data changed.
- The §step-4 question (substituting a proven Class W advisory such as
  GHSA-6w3j for GHSA-67hx) is moot: GHSA-67hx itself now has the live
  both-lines replacement. No ruling change needed.

## Step 5 — retire transaction (READY pending coordinator go-ahead)

Step 4 collection is proven above; the impact flip (AC3.3b) follows the
commit per the ruling. Run script Section 4 with `-v retire_ok=1` after
a fresh Sections 0–3 dry run at execution time (the retire set is
recomputed then; in-flight membership fluctuates). Rollback key captured
from output.

## Step 6 + AC3 (PENDING step 5)

Pre-retire baseline (script Section 6, same run as dry run): AC3.1 = 72,
AC3.2 = 497, AC3.3a = 0 satellite rows, AC3.3b = 2 x `possibly_affected`,
AC3.4 = 419. Post-commit targets: 0 / 0 / both-lines-present / `affected` /
≥419, plus reader-surface check (AC3.5).

## Rollback window

Opens at the Section 4 commit; closes at the first retention prune of a
marked row or a satellite activation on the same scope. State the closing
here when it passes.

## Hardening (#9597 — closes review threads on #9595)

Two findings from the #9595 review (threads 4420015885/4420068975 on the
in-flight recheck, 4420015889/4420068981 on the rollback) were valid at
merge and are fixed in `9431-qa-retire.sql` before the authorized Section 4
QA run. No QA state changed: all proof below ran on a scratch Postgres 18
database with dummy instance ids.

- **In-flight gate.** Sections 3 and 4 capture the locked recheck with
  `\gset` (`inflight_n`, `abort_inflight` boolean) and an `\if`-gated DO
  block RAISEs under `ON_ERROR_STOP`, so a nonzero count aborts the `-f`
  run with exit 3 BEFORE any `UPDATE`. Both `UPDATE`s additionally carry
  `AND NOT EXISTS (claimed/running/retrying for the retire set)` so a
  claim landing between recheck and write still cannot be written. The
  Section 3 vs 4 lock+gate+`UPDATE` bodies are line-identical (diff shows
  only the reporting tail and `COMMIT` vs `ROLLBACK` differing).
- **Standalone rollback.** Section 4 writes `retired_prior_scope_status`
  into the durable generation payload marker before `COMMIT` (same
  authorized columns/rows, no DDL). Section 5 is executable SQL gated on
  `-v rollback_ok=1` with zero session-local references (no `pg_temp`;
  verified by scan), restoring scope pointers from the marker first, then
  reactivating only generations whose scope now points back at them.
  `rollback_marker_remaining` must read 0; anything higher names rows
  whose window closed.

Scratch proof (script run as committed, `-v retired_instance/-v
satellite_instance` dummy values; psql 18.6, `ON_ERROR_STOP=1`):

| Run | Setup | Result |
|---|---|---|
| RED (base file) | claim injected after classify | recheck printed 1, `UPDATE 2/UPDATE 2` still executed, exit 0 — the reported hole |
| S3 abort (race) | claim injected strictly between classify and recheck, retire set of 2 | `ABORT` + `ERROR: retire ABORTED`, exit 3, 0 marker rows, both generations active, 0 NULL pointers |
| S3 clean (full file, no gates) | no in-flight | exit 0, `dryrun_updated_generations=2`, `dryrun_marker_prior_status=2`, `dryrun_cleared_scopes=2`, rolled back to 0 markers / 0 NULL pointers |
| S4 abort (race, `retire_ok=1`) | claim injected mid-window | `ABORT` + `ERROR`, no `UPDATE`/`COMMIT`, exit 3, 0 markers committed |
| S4 commit (`retire_ok=1`) | no in-flight | exit 0, `COMMIT`, 2 generations `superseded` with `retired_issue=9431` + `retired_prior_scope_status=active`, 2 scope pointers NULL |
| S5 fresh session (`rollback_ok=1`) | new psql session after the commit | exit 0, scopes back to `active` with pointers restored, generations `active`, payloads back to `{}`, `rollback_marker_remaining=0` |
| S5 window-closed | one scope re-pointed at a new active generation before rollback | that scope untouched (old generation stays `superseded` with marker, pointer stays new), the other scope restored, `rollback_marker_remaining=1` |

## Follow-up review on #9603 (P1 + P2, fixed before merge)

Two findings arrived on the PR itself and are fixed in the same files:

- **P1 — all-or-nothing retire.** The second-layer `NOT EXISTS` predicates
  skip a raced row per statement, so a claim committed after the recheck
  under `READ COMMITTED` retired the rest of the set with exit 0 (proven RED
  on the committed head: recheck 0, `UPDATE 1`/`UPDATE 1`, `COMMIT`, one
  generation retired while the raced scope stayed active). Sections 3 and 4
  now re-count non-fully-retired rows after the `UPDATE`s and raise before
  the section tail on any shortfall. GREEN with a claim committed strictly
  between recheck and `UPDATE`s: `UPDATE 1`/`UPDATE 1` (raced row skipped),
  `must_be_zero_shortfall=1`, `ABORT` + `ERROR`, exit 3, zero rows committed
  (both generations active, no markers, no cleared pointers). The remaining
  check-to-tail window holds no writes, only the read-only key `SELECT`.
- **P2 — falsy gate values.** `:{?var}` tests definedness, so `-v
  retire_ok=0` took the run path (silently skipping via a misbound `\else`;
  no write occurred, but the operator got no message). Both gates now test
  defined AND truthy (`lower(value) IN (1/true/t/on/yes/y)`, unset and falsy
  values print the `SKIPPED` line). Proof: `-v retire_ok=0`, `rollback_ok=0`,
  and `retire_ok=off` each exit 0 with both sections skipped and zero
  markers; `-v retire_ok=TRUE` runs the retire (`COMMIT`, 2 markers).
- The Section 3 vs 4 lock+gate+`UPDATE`+check bodies remain line-identical
  (diff shows only the reporting tail and `COMMIT` vs `ROLLBACK`); the
  `=1`-only wording now says defined-and-truthy in the header, usage
  comment, and both section headers.

## Tail in-flight recheck (follow-up P2 on #9603, fixed before merge)

A further review round noted the shortfall check reads generations and
scopes but never `fact_work_items`, so a claim committed after the last
`UPDATE` but before `COMMIT` was still invisible to every gate. Both
sections now re-read `fact_work_items` for the retire set as the last gate
before the section tail and raise on any hit.

- RED on the prior head (shortfall-only): with a claim committed strictly
  after the `UPDATE`s, both `UPDATE`s ran (`UPDATE 2`/`UPDATE 2`),
  shortfall read 0, and the run `COMMIT`ted with exit 0 — two generations
  retired while a claimed work item pointed at one of them.
- GREEN on the fixed head, same injection point: `UPDATE 2`/`UPDATE 2`,
  shortfall 0, tail recheck 1, `ABORT` + `ERROR`, exit 3, zero rows
  committed (both generations active, no markers, no cleared pointers).
- Residual window is narrowed to the tail statement's snapshot versus
  `COMMIT` (only the read-only key `SELECT` sits between them in Section
  4). Full closure needs claim-path coordination, out of scope for this
  one-shot script; a claim landing inside the residual window still cannot
  resurrect rows (superseded stays terminal, the scope pointer stays NULL),
  and the worker-side fence re-validates at processing time.

## Step 5 — retire transaction (COMMITTED 2026-10-31 ~16:00 UTC, gate PASS)

Authorization: arbiter APPROVE ruling (subset-plus-accounting gate bound to
the COMMIT invocation) plus the owner production-write approval; together
they cover this COMMIT. Scope is unchanged from the original ruling: QA
only, predicate rows only, no DELETE/DDL, no worker changes, no script
edits. Script identity: `diff -q` of
[9431-qa-retire.sql](9431-qa-retire.sql) against the main checkout exits 0;
the committed script ran unmodified.

Same-window mechanism proof first: fresh Sections 0–3 dry run
2026-10-31 ~15:59 UTC (committed script, QA primary, exit 0, Section 4
SKIPPED without `retire_ok`, ROLLBACK): classification
`candidates=136 classW=71 classS_eligible=18 mixed=0 inflight=1`, retire set
89 (71 W + 18 S, 1 in-flight blocker), `dryrun_*=89/89/89` reconciled,
0 markers and 0 NULL-pointer OSV scopes after. Its N is evidence only.

COMMIT invocation 2026-10-31 ~16:00 UTC (`-v retire_ok=1`, exit 0):
classification `candidates=136 classW=71 classS_eligible=18 mixed=0
inflight=1`; Section 3 dry-run body `UPDATE 89/89` then ROLLBACK; Section 4
lock + gate + `UPDATE 89/89`, all six `must_be_zero_*` gates 0
(inflight-locked, shortfall, inflight-tail in both sections), rollback key
89 rows, `COMMIT`; Section 5 SKIPPED; Section 6 post-commit reads
AC3.1=1, AC3.2=27, AC3.4=826. Zero `ABORT`/`ERROR`/`FATAL` in the log.

Restated gate verified from THAT invocation's log plus read-only queries
immediately after (exit 0), all PASS:

| Condition | Evidence |
|---|---|
| `mixed=0` | Classification line `mixed=0`; mixed listing `(0 rows)`. |
| `candidates=136` | Classification line `candidates=136` (any drift was a STOP; none). |
| Every committed W in oracle 72 W | Committed 71 W-flagged rows; committed-minus-oracle is 0 rows; the single oracle-minus-committed row is W-flagged, so committed W = oracle W minus that row. W flags recomputed live per row (facts untouched by the commit). |
| Every committed S fully covered at classify | Committed S = 18 = oracle S (no S row beyond the oracle 18); script classify marked all 18 eligible and retired them; live recompute immediately post-commit: 18/18 fully satellite-covered. |
| Every oracle row not committed accounted | Exactly 1: `vuln-intel://osv/npm/axios?...` / `vulnerability_intelligence:defdbf...`, present in the COMMIT run's Section 1 in-flight listing (claimed at classify, excluded fail-closed). |
| Marker count = committed N; rollback key captured | Live `retired_issue='9431'` count = 89; NULL-pointer OSV scopes = 89; all 89 committed rows superseded + pointer cleared + `retired_prior_scope_status` present (all values `active`). Key in the Section 4 log output and durably in each payload marker. |

Per-row membership (89 rows: scope, generation, prior status, W/S class,
oracle member): [9431-step5-commit-members.tsv](9431-step5-commit-members.tsv).
Scope ids in that file redact one proprietary npm org scope as
`REDACTED-ORG` (3 rows redacted, generation ids and classes intact); the
unredacted key is in the COMMIT log and the durable payload markers.
Both-direction diff: committed-minus-oracle 0 rows; oracle-minus-committed
the single axios in-flight row above (W, excluded fail-closed).

Immediate post-commit state (step 6 pending): AC3.1=1 (axios only, still
active + in-flight), AC3.2=27 (down from 497), both GHSA-67hx-6x53-jw92
scopes (`@babel/runtime`, `@babel/traverse` batches) retired with markers —
the step-4 replacement is now the only live line. Per the APPROVE ruling
decision 3 this partial pass does NOT close #9431: repeat the gated pass as
the remaining scope drains (axios), close only when AC3.1/AC3.2 read 0
after step 6.

Rollback window: OPEN at commit (no prune or satellite activation on a
retired scope observed at verify time ~16:01 UTC). Closing is recorded here
when it passes.
