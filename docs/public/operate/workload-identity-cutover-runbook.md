# Workload Identity Cutover Runbook (#5385)

One-shot, drained cutover from name-only Workload/WorkloadInstance keys
(`workload:<name>`) to repository-scoped keys (`workload:<repo_id>:<name>`).
Design authority: `docs/internal/design/5385-workload-identity-migration.md`
§3 (ordered cutover), §4 (writer set), §7 (consumers), §8 (telemetry), §9
(probes). Step-1 code ships in the same release PR as the step 6 flip (two
commit groups per the single-PR ruling on #5385); this runbook covers
operational steps 2–9 and rollback in a single operator session with a
stated time bound.

## Preconditions (all verified before step 2)

- [ ] Binary at the cutover release (constructors emit scoped ids, cascade
  included) staged on every reducer lane, **not yet rolled**. HARD NO-ROLL
  GATE: no lane runs the new binary until step 6; a lane found on the new
  binary before step 6 aborts the run (coexistence window, migration §3).
- [ ] Stale-`USES` hazard gate closed as a hard pre-cutover gate — either
  proven on Neo4j or recorded open with owner sign-off. (This is the
  compatibility-doc stale-`USES` hazard, not the migration §6 rebuild
  wall-time probe that shares the P3 name — the wall-time probe is measured
  at cutover under the stated time bound (steps 5 and 7), not a pre-cutover
  gate.) Do not start step 2 with the hazard undecided.
- [ ] In-PR evidence on the release PR: T1 (scoped-token
  `same_name_siblings = 0`), T2 (recomputed route counts), T3 (per-consumer
  full/unique/spanning tables), T4, T5 (handle ladder tables), P4 (plan
  pin), P5 (two-repository Odù on the old key: one node, counters fire).
- [ ] Time bound stated and announced: _____ (operator fills; migration §3
  requires one stated bound for steps 2–9).
- [ ] Read-outage window announced: from step 3 until step 8, every
  workload-rooted read (`/workloads/{id}/context`, `/story`,
  `/compare/environments`, runtime topology, `/catalog` workload rows,
  impact traces through a workload) returns empty or 404.

## Step 2 — Hold the claim domains

Restart every reducer lane with `ESHU_REDUCER_CLAIM_DOMAINS` set to an
explicit include-list that omits the §4 claim set (every `claim` row plus
the side-runner emitters: `code_call_materialization` rows 4–5,
`code_import_repo_edge`, `package_source_correlation`, `deployment_mapping`).
The include-list is applied to the claim SQL; a lane with it unset claims
everything. The two side runners go quiet by construction once emitters are
held and the backlog drains (no worker-count knob holds them).

Verify: each lane's effective claim list logged at startup omits the held
domains.

## Step 3 — Drain both queues for the held set

- `fact_work_items`: every held-domain row leaves `claimed` and `running`.
- `shared_projection_intents`: `handles_route`, `runs_in`, and
  `repo_dependency` read zero outstanding and zero in flight, via
  `domain_backlogs` on `GET /api/v0/status` with an all-scopes token, or
  directly:

  ```sql
  SELECT projection_domain, count(*)
  FROM shared_projection_intents
  WHERE completed_at IS NULL GROUP BY 1;
  ```

Settled-zero on both reads is the gate to step 4. The held set includes the
service-catalog correlation domain (its `service_evidence_key` embeds the
instance id).

## Step 4 — Retire durable rows keyed on the old ids

Using the old ids: `service_evidence_key` rows and persisted search handles
(`go/internal/searchdocs/project.go`). Postgres and the search index coexist
with the graph until step 8: findings keep old handles until regeneration,
search returns old handles until reindexed — both read through the ladder
(resolve or fail closed, never silently miss).

## Step 5 — DETACH DELETE by label

Delete `Workload`, `WorkloadInstance`, `Endpoint` **by label, not by id**
(an edge that escapes an id-anchored delete stays forever). Verify with a
settled count read of zero per label (the ledger rows record the stale-read
trap on this backend — re-read until settled).

## Step 6 — Roll the flipped binary (irreversible)

Roll the cutover release (flipped constructors + retirement-scoped
repository-cleanup cascade in the same roll, so the cascade never executes
under the old key). This is the only irreversible edit in the sequence.

## Step 7 — Reopen and re-run (order matters)

With the step 2 lanes **still held**:

1. `POST /api/v0/admin/recover-generations` with `all_scopes: true`, a
   `reason`, and a fresh `idempotency_key` (admin token).
2. Wait for HTTP 200. Only then restore the lane lists from step 2.

Wrong-order recovery: lanes restored first claim the held backlog, hold live
leases, and can turn the request into a drain-timeout error — issue a fresh
`idempotency_key` and repeat in the right order. Read `skipped_scopes` in
the response (`RefinalizeResult.Skipped`, reported by reason): a skipped
scope lost its nodes in step 5 and stays empty until hand-projected; the
step 9 count assertion catches it only after the flip.

## Step 8 — Close the window

Regenerate the golden artifacts (§7), re-prove the
`FetchWorkloadRuntimeTopology` plan pin (P4), reindex search.

## Step 9 — Two-repository Odù on the new key

Counters at zero, two nodes, one `SAME_NAME` edge, per-family edge counts
equal to the pre-cutover collision-free baseline. Closes the window opened
in step 3.

## Rollback

Rollback repeats steps 2, 3, and 5 **before the old binary writes anything**,
then rolls the old binary (the cascade needs no rollback of its own), then
with lanes still held issues the step 7 request again with a fresh key,
restores lane lists after the 200. `service_evidence_key` rows regenerate
from facts when the service-catalog runtime domain re-runs; search handles
reindex via `eshu_search_document` (same reindex as step 8, under the old
binary). Past step 8, additionally revert the golden regeneration. The step
5 delete is not the point of no return; facts are.

## After the cutover: leftover nodes

If a deployment still holds name-only `Workload` nodes after step 9, hosted
readiness fails `workload_identity_leftovers`. Retire them with the
[leftover retirement runbook](legacy-workload-retirement-runbook.md); it is
dry-run first and never runs automatically.
