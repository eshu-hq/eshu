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

## Step 5 — retire transaction (PENDING step 4)

Runs script Section 4 with `-v retire_ok=1`; rollback key captured from output.

## Step 6 + AC3 (PENDING step 5)

Pre-retire baseline (script Section 6, same run as dry run): AC3.1 = 72,
AC3.2 = 497, AC3.3a = 0 satellite rows, AC3.3b = 2 x `possibly_affected`,
AC3.4 = 419. Post-commit targets: 0 / 0 / both-lines-present / `affected` /
≥419, plus reader-surface check (AC3.5).

## Rollback window

Opens at the Section 4 commit; closes at the first retention prune of a
marked row or a satellite activation on the same scope. State the closing
here when it passes.
