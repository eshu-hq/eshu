# Workload-Materialization Per-Repo Readiness Key Evidence (#2891)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Workload-materialization per-repo readiness key (#2891)

`HANDLES_ROUTE` and `RUNS_IN` edges never projected even after the #2844 presence
fix: their workload-materialization phase gate could never match. The readiness
lookup (`graph_projection_phase_state.go`) is an EXACT match on
`(scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)`,
but the `handles_route`/`runs_in` intents run in the CODE stage and built their
lookup key from their own acceptance unit and code-stage `source_run`, while the
`workload_materialization` phase is published in the WORKLOAD stage under that
intent's EntityKeys and its own `source_run` (= its generation). Acceptance unit
(`repository:` vs the workload intent's basename `repo:`/`workload:` EntityKey)
AND `source_run` (code run vs workload run) both mismatched, so the lookup missed
forever and every row deferred at the phase gate — the working #2844 presence
second-gate was never reached. `code_calls` was unaffected because its intent and
its prerequisite phase are both published by the code stage with identical keys.

The fix is one deterministic helper,
`workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)`
(`shared_projection_readiness.go`), used on BOTH sides so they cannot drift:
`AcceptanceUnitID = repoID` (the `repository:<id>` graph_id the intent carries as
`repo_id` and the workload candidate carries as `RepoID`), `SourceRunID =
GenerationID` (so a code-stage row reconstructs it without knowing the workload
stage's run), `Keyspace = service_uid`. The workload-materialization handler
(`workload_materialization_handler.go`) ADDITIVELY publishes one phase row under
this key per distinct repo it materialized endpoints/workloads for on the
candidate path, and per repository `graph_id` in scope on the zero-candidate path
(so a route-only repo's gate still resolves) — the existing per-EntityKey workload
phase publications are kept for the other consumers that depend on them. The
consumer (`filterRowsByReadiness`) derives the lookup key via this helper for
`DomainHandlesRoute`/`DomainRunsIn` only, in BOTH the prefetch key-collection and
per-row lookup loops; every other domain still uses
`graphProjectionPhaseKeyForIntent` byte-identically. The #2844 presence
second-gate (`symbolRuntimePresenceGate` + `filterRowsByTargetPresence`) stays
unchanged and authoritative: among phase-ready rows it still decides
present(project) vs absent(terminal), so a non-deployed/route-only repo never
silently projects a wrong edge.

No-Regression Evidence: `go test ./internal/reducer -run
'RepoReadiness|FilterRowsByReadiness|WorkloadMaterializationHandler|HandlesRoute|RunsIn'
-count=1` — `TestWorkloadMaterializationRepoReadinessKeyRoundTrips` proves the
publisher and consumer keys are byte-equal for the same `(scope, repo,
generation)`; `TestFilterRowsByReadinessHandlesRouteResolvesViaRepoKey`
reproduces the live mismatch (intent au=`repository:r_…`/srun=code-run) and proves
the row now resolves when the ONLY published phase row is the repo-keyed one;
`TestFilterRowsByReadinessHandlesRouteOldStyleKeyStillMisses` proves an old-style
`repo:<id>`/srun=gen row alone does NOT resolve it (no silent edge);
`TestFilterRowsByReadinessCodeCallsKeyUnchanged` proves `code_calls` keying is
unchanged and the new repo-keyed row does not resolve it. The change is additive:
on the candidate path it appends one phase row per materialized repo (no extra
fact load); on the zero-candidate path it adds one bounded `repository`-kind fact
query (one row per repo). It introduces no new graph write, Cypher, lease, or
worker, and the non-symbol-runtime key derivation is untouched. `go test
./internal/storage/postgres ./cmd/reducer -count=1` confirms the durable phase
store and reducer wiring are unaffected. The publisher additionally retracts each repo's stale (other-generation) presence in both symbol-runtime keyspaces before opening its phase gate, so a generation with no endpoint/workload row for a repo cannot let the presence gate see a prior generation's target as present and project a stale edge (`TestPublishRepoReadinessPhasesRetractsStalePresenceBeforeOpeningGate`).

Observability Evidence: an operator sees the fix as `handles_route`/`runs_in`
shared-projection intents completing (`Function-[:HANDLES_ROUTE]->Endpoint` and
`Function-[:RUNS_IN]->Workload` edges appearing) instead of looping on "skipped
intents until semantic readiness is committed", while `workload_materialization`
intents continue to `succeed` and `graph_endpoint_presence`/repo-workload presence
rows continue to be written. The repo-keyed readiness rows land in
`graph_projection_phase_state` under `acceptance_unit_id = repository:<id>`,
`source_run_id = <generation_id>`, `keyspace = service_uid`, `phase =
workload_materialization`, alongside the existing per-EntityKey rows. No new
metric instrument, label, span, or log line is added.
