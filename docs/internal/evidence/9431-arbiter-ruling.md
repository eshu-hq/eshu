> Lane redaction note (2026-10-30): the two QA collector-instance ids in
> this ruling are replaced below with role names to satisfy the
> no-private-identifiers gate. The literal values are recorded on #9431
> (2026-10-29 evidence comment) and passed to the retire script as psql
> variables. No normative content is changed.

# Arbiter ruling — #9431 stale OSV generation retirement

RULING: (b) one-time operational retire on QA, replacement-first, with the
retire script and proof queries committed for review; (c) alone and (a) for
this issue are rejected; no Go code change.

Arbiter: arbiter-eshu (muse-wonder-full-1.0), 2026-10-30. This supersedes the
unlocatable `9209-wave1-arbiter-9431-20261028` s3.3 reference; the lane records
this file's committed path on #9431 and in the PR. Every source claim below was
re-read by the arbiter in `/home/ubuntu/personal-repos/eshu` at ruling time.

## Verified facts (source, re-read by arbiter)

1. Retention never retires an active generation. The policy bounds only
   superseded source-local generations; "the active generation and the newest
   superseded generations inside the count or age window are never candidates"
   ([generation_retention_policy.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/generation_retention_policy.go#L42)).
   A prune candidate must have `status='superseded'` AND
   `generation_id IS DISTINCT FROM scope.active_generation_id`
   ([targeted_candidate_lock.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/retention/statements/targeted_candidate_lock.go#L116)).
   A scope never collected again keeps its stale active generation forever.
   Waiting is not a mechanism.
2. Supersede is same-scope-only, at ack:
   `supersedeProjectorActiveGenerationQuery` updates `WHERE scope_id = $2 AND
   generation_id <> $3 AND status = 'active'`
   ([projector_queue_sql.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/projector_queue_sql.go#L43)).
   Superseded is terminal for the ack/replay paths
   ([projector_queue_sql.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/projector_queue_sql.go#L53),
   fence at :349).
3. OSV scope ids are the batch string: `vuln-intel://osv/%s/_batch?` plus
   `query=<name>@<version>&...`
   ([intelligence_chunk_sizer.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/coordinator/vulnerability/intelligence_chunk_sizer.go#L21),
   :59). Old batches do not recur except by coincidence (lane measured 3/139),
   so ack-supersede will not reach these generations.
4. The data loss was the retired collector's stable key, not a storage bug.
   `deduplicateEnvelopes` keep-last-per-fact-id is the documented contract
   ([facts.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/facts.go#L345),
   body :355-375). It stays unchanged.
5. The impact matcher reads only facts whose generation is the scope's active
   one ([branches.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/facts/impact/branches.go#L77),
   predicate :81, :91). Clearing `active_generation_id` removes a
   generation's facts from matcher input on the next normal reducer cycle. No
   new notification machinery is needed.
6. No generation→instance link exists for a general rule to join on.
   `scope_generations` has no collector instance column
   ([002_scope_generations.sql](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/schema/data-plane/postgres/002_scope_generations.sql:1));
   the instance stamp lives on facts. A collector instance registry with
   `deactivated_at` does exist
   ([015_workflow_coordinator_state.sql](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/schema/data-plane/postgres/015_workflow_coordinator_state.sql:1)),
   but nothing joins it to generations, and no deactivated-instance rule
   exists in retention SQL, policy, or wiring (arbiter searched
   `deactivat|decommission|retire` over `go/internal/storage/postgres`).

## Decisions

1. **Mechanism: (b), one-time operational retire on QA, in two classes.**
   - Class W (wrong): active retired-instance OSV generations holding a
     multi-entry advisory (provably partial ranges). Retire unconditionally
     after the replacement step below. Wrong data does not stay because a
     replacement is missing; the issue's Expected explicitly allows "readers
     do not see the stale partial facts."
   - Class S (stale-but-not-proven-wrong): single-entry retired-instance
     generations. Retire only those with a live satellite replacement at run
     time. The rest stay active, are listed in the evidence as the
     stale-instance remainder, and get a follow-up issue for re-collection.
     Retiring correct advisories into a coverage hole trades accuracy away;
     that is not accepted.
2. **(c) forced re-collection alone: rejected.** Fact 3: new batches mint new
   scope ids, so re-collection adds correct facts beside the stale active
   ones and retires nothing. It is a step inside (b), not the mechanism.
3. **(a) general retention/liveness rule: rejected for #9431.** Fact 6: it
   needs generation→instance attribution that does not exist, plus
   prove-the-theory measurement on the hot retention path, plus semantics
   for sole-source static scopes. That is a feature, filed as its own issue
   if wanted; it does not block closing #9431.
4. **No Go code change.** The PR carries: the retire script (exact SQL,
   reviewed before it runs), the candidate/proof queries, and the evidence
   record. Hand-typed psql from memory is not authorized; the reviewed,
   committed script is what runs.
5. **No DELETE, no DDL.** Retire = `status='superseded'` +
   `superseded_at` + clear `ingestion_scopes.active_generation_id` + payload
   audit marker (below). Normal retention prunes the rows later under the
   existing policy. That is the designed path consuming the result — this is
   using retention as built, not bypassing it.

## Authorization (data change)

This ruling authorizes, on the **QA environment only** (the environment the
acceptance criteria name): UPDATE of `scope_generations.status`,
`superseded_at`, `payload` marker, and `ingestion_scopes.active_generation_id`
(and scope `status` per step 4) for rows matching the candidate predicate in
the committed script. It does not authorize: any other environment, DELETE,
DDL, hand-written facts, or writes outside the script's predicate. Any other
environment needs its own recorded authorization reusing this script and
proof.

## Exact steps (lane executes; each step's output goes in the evidence)

1. **Satellite precondition.** Record the deployed QA satellite build and
   prove #9421 behavior live: one multi-entry advisory recollected by
   the QA satellite instance carries every `affected[]` entry. If not proven,
   stop; retiring into a still-broken collector recreates the bug.
2. **Candidate set, fail-closed.** Active generation
   (`scope.active_generation_id = g.generation_id`, `g.status='active'`),
   `scope_id LIKE 'vuln-intel://osv/%'`, every live
   `vulnerability.affected_package` fact in the generation stamped
   the retired in-tree QA instance, none stamped any other instance
   (mixed-instance generation → exclude and report), and no in-flight work
   (`fact_work_items.status IN ('claimed','running','retrying')`, mirroring
   [targeted_candidate_lock.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/retention/statements/targeted_candidate_lock.go#L140)).
   Class W = candidates holding an advisory on the multi-entry list: the
   `eshu-9421-scan` list if locatable, else regenerated read-only against
   api.osv.dev at run time; the list is committed either way.
3. **Dry run.** Whole retire inside `BEGIN … ROLLBACK` first. Record counts
   per class, reconcile against the lane baseline (574 active
   retired-instance generations, 1115 facts — re-measured at run time, never
   hardcoded), explain any drift or abort. In this step the lane also cites
   `ingestion_scopes.status` consumers and picks the value an existing
   no-active-generation path writes (precedent: `failProjectorWorkQuery`
   clears the pointer with `status='failed'`,
   [projector_queue_sql.go](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/go/internal/storage/postgres/projector_queue_sql.go#L239));
   no invented status value.
4. **Replacement pass.** Run/await one normal coordinator + satellite cycle.
   GHSA-67hx-6x53-jw92 (`@babel/traverse`) must end with satellite facts
   carrying both `[0, 7.23.2)` and the 8.0.0-alpha line; if the natural cycle
   misses the deployment pinning 7.29.7, drive that package through the
   existing coordinator derivation — never hand-written facts.
5. **Retire transaction.** One transaction; lock candidates in
   `(scope_id, generation_id)` order `FOR UPDATE` (retention lock doctrine);
   set `status='superseded'`, `superseded_at=now()`,
   `payload = payload || {"retired_issue":"9431","retired_at":…}` (payload
   column: [002_scope_generations.sql](https://github.com/eshu-hq/eshu/blob/761f65e2f717e861e651cecec1b4eb5e1e635ec3/schema/data-plane/postgres/002_scope_generations.sql:13));
   clear the scope pointer per step 3. Export `(scope_id, generation_id,
   prior scope status)` to the evidence before commit as the rollback key.
6. **Re-trigger.** Await the next normal reducer supply-chain-impact cycle
   after the commit (fact 5 makes it sufficient) and record its timestamp.

## Required proof (closes the acceptance criteria)

- AC1 (ruling recorded): this file committed; its path linked on #9431 and in
  the PR. Closes on commit, before any QA write.
- AC2 (retention rule read from source): facts 1–2 above plus the lane's
  2026-10-29 comment. Closed now.
- AC3 (QA re-query), all read-only, after step 6, with exits and timestamps:
  1. Candidate predicate re-run returns **0 Class W rows**.
  2. Zero live `vulnerability.affected_package` facts from
     the retired in-tree QA instance reachable via
     `active_generation_id` for Class W advisories.
  3. GHSA-67hx-6x53-jw92: live satellite facts carry both release lines, and
     the impact answer for the 7.29.7-pinned deployment reads `affected` —
     not `possibly_affected` / `version_not_in_advisory_range`.
  4. No collateral: live satellite fact count ≥ its run-time baseline
     (389 in the lane baseline, re-measured at step 2).
  5. Reader surface: the same advisory through the API/readiness path shows
     the 7.x range. If the graph still shows only the 8.x range after 1–4
     pass, that is a named follow-up finding on the PR, not silence — the
     arbiter did not trace graph-side retraction for pointer-cleared scopes.

## Rollback

Before the first retention prune touches a retired row: operational SQL (not
ack/replay — superseded is terminal there, fact 2) restoring
`status='active'`, `superseded_at=NULL`, and the pointer, restricted to rows
carrying the step-5 marker, still superseded, with the scope pointer still
NULL. After a prune, or once a satellite generation activates on the same
scope, rollback is re-collection only. State the closing of the rollback
window in the evidence when it passes.

## What will not be done, and why

- No general "deactivated collector" retention rule in this issue (decision 3).
- No change to `deduplicateEnvelopes` (fact 4 — documented contract; the bug
  was the retired collector's key, fixed satellite-side by #9421).
- No worker serialization, batch-size, or retention-policy change of any
  kind; nothing here is a concurrency workaround.
- No DELETE of facts or generations (decision 5).

## NOT_VERIFIED by the arbiter

- QA live counts (574 generations, 1115 vs 389 facts, GHSA envelopes,
  `possibly_affected` finding): the lane's 2026-10-29 read-only measurements,
  not re-run from this seat. Steps 2–3 re-measure before any write.
- The deployed QA satellite actually carrying the #9421 fix (issue is
  CLOSED; it lives outside this repo — `gh pr list --search 9421` in
  eshu-hq/eshu returns none). Step 1 is the gate.
- Graph/API-side retraction when a scope pointer is cleared without a
  successor generation (proof item AC3.5 owns this).
- Coordinator coverage of all 265 multi-entry pairs. AC3 requires the
  dropped-line proof, not full coverage; uncovered Class W advisories end
  absent-not-wrong per the issue's Expected, and the Class S remainder list
  names what still needs re-collection.
- The count discrepancy between the issue body (72 of 138 active
  generations, 2026-10-28 scan) and the lane baseline (574, 2026-10-29):
  different predicates (multi-entry-holding vs all retired-instance).
  The two-class rule in decision 1 is written against the wider predicate;
  the dry run must show both numbers reconciled.
