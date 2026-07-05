# First-projection whole-scope retract skip (#3624)

Classification: **Wall-clock win** (cold git-collector E2E) + completion-correctness
win (the pre-change drain never reached repository projection in bounded time).
Not a correctness change to graph truth: the skipped work is a provable no-op.

## Root cause

For the repo-wide-retract shared-projection domains (`sql_relationships`,
`inheritance_edges`, `shell_exec`, `handles_route`, `runs_in`,
`invokes_cloud_action`, `rationale_edges`), each per-repo refresh intent issues a
whole-scope edge retract before writing edges. On NornicDB that retract MATCH
carries a property predicate (`source.repo_id IN $repo_ids` and/or
`rel.evidence_source = $evidence_source`), which defeats the relationship-type
index and forces a full node/relationship store scan. Measured on a live
full-corpus backend (1,109,200 nodes / 1,739,506 rels, `:Function` = 572,358),
via the NornicDB neo4j-compatible HTTP tx endpoint (`count(rel)` shim):

- `MATCH ()-[r:QUERIES_TABLE]->() RETURN count(r)` (empty type): 0.0016s
- `MATCH ()-[r:HAS_COLUMN]->() WHERE r.evidence_source=$es`: 33.9s
- `MATCH (s:Function)-[r:QUERIES_TABLE]->() WHERE s.repo_id IN [1 repo]`: 13.8-28.5s (per-repo, not cached)
- `MATCH (child)-[r:EXTENDS]->() WHERE child.repo_id IN [1 repo]`: 33-47s (per-repo)

That is ~15-47s per repo × ~6 edge types × ~910 repos = 4-10h+. On a cold first
ingest every one of these retracts deletes **zero** edges (nothing prior exists)
yet still pays the full scan. The systemic NornicDB planner defect (property
predicate ⇒ full scan) is tracked separately (#4708); it is the warm-re-ingest
half and is not addressed here.

## Fix

Skip the whole-scope retract for a per-repo refresh intent when the intent's
scope has **no generation other than the current one** (`scope_generations` with
a different `generation_id`, in any status). These shared-projection domains
write their edges on **acceptance**, before a generation activates (the runner
is wired with the raw accepted-generation lookup, not the activation gate), so a
generation that was accepted and then superseded while still pending can have
written edges without ever setting `activated_at`. The correct zero-prior-edges
signal is therefore "the scope has never had another generation" — a true
first-ever projection — not "no other *activated* generation". A scope with any
other generation (active, superseded, pending, or failed) may already hold edges
and is not skipped, so every re-ingest still retracts. (Orphan edges from the
general acceptance-before-activation window are additionally converged by the
#3559 dual-write reconciliation, but this skip does not rely on that backstop —
it never skips when a prior generation exists.) The refresh row still lands in
`completedRows` so the durable refresh fence opens and per-edge writes proceed
unchanged; only the retract call is skipped. A nil `FirstProjectionLookup`, an
empty scope id, or any probe
result of "a prior generation exists" leaves the retract running
byte-identically (so every re-ingest still retracts).

## Performance Evidence:

Measured against a resident full-corpus backend (Postgres 18 + NornicDB
`eshu-nornicdb-main:d97f02c1`, `nornic` db; 910 scopes, all activated; 5.16M
`fact_records`; frozen backlog sql_relationships=8698, inheritance_edges=7591,
shell_exec=516 pending). Same reducer config (`ESHU_REDUCER_WORKERS=16`), same
240s window, same backlog, current-main vs this branch:

| domain | BEFORE (main) completions/240s | AFTER (this branch) completions/240s |
|---|---|---|
| sql_relationships | 0 | 800 |
| inheritance_edges | 0 | 498 |
| shell_exec | 0 | 433 |
| handles_route | 0 | 9 |
| total | 0 (drain wedged) | ~1740 |

BEFORE: the current-main reducer drained **0** repo-wide-retract-domain intents
in 240s (pending unchanged 8698→8698 for sql_relationships), wedged in the
per-repo full-scan retracts — this reproduces the #3624 long pole. AFTER: the
same worst-case backlog drains freely; 783 whole-scope retracts were skipped as
first-projection no-ops in the window (444 inheritance_edges, 306 shell_exec, 28
sql_relationships refresh intents completed without their retract). The skipped
retracts are equivalence-preserving by construction: first-projection scopes
have zero prior edges, so the retract deletes nothing whether run or skipped.

Regression coverage (all green, `go test ./internal/reducer/ -count=1`):
`TestPlanRepoWideRetractWorkReIngestStillRetracts` (a scope with a prior
generation still retracts — the safety case),
`TestPlanRepoWideRetractWorkSkipsFirstProjectionRetract`,
`TestPlanRepoWideRetractWorkNilFirstProjectionPreservesLegacyBehavior`,
`TestPlanRepoWideRetractWorkMemoizesPerScope`,
`TestPlanRepoWideRetractWorkPropagatesProbeError`,
`go test ./internal/storage/postgres/ -run 'ScopeHasPriorGeneration|FirstProjectionLookup'`,
and the live-Postgres proof
`ESHU_FIRST_PROJECTION_PROOF_DSN=... go test ./internal/storage/postgres/ -run TestScopeHasPriorGenerationAgainstPostgres`
(false with only the current generation; true once a superseded-while-pending
generation that never activated exists — the exact case a naive activated_at
filter would miss).

## Observability Evidence:

Each skip emits a structured operator log at INFO,
`"skipped whole-scope retract on first projection"`, carrying `domain`,
`repo_id`, `scope_id`, `generation_id`, and the active trace/span ids (observed
783 times in the after-run). Operators also see the existing
`RetractDurationSeconds` shared-projection telemetry drop to ~0 for
first-projection refreshes. No new `eshu_dp_*` metric was added; the skip is a
subtraction of work made observable by the log plus the pre-existing retract
duration signal.

## Scope note

This fixes the cold/first-projection path only. Warm full re-ingest of a changed
repo still runs the property-predicate retract (correctly, since prior edges
exist) and remains slow until the NornicDB systemic fix (#4708) lands.
