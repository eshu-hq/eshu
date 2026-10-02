# Reader fence failures answer a retryable 503 (#7523)

Refs #7249. A PostgreSQL reader that has not replayed to the writer checkpoint
within the replay timeout (`ErrReaderStale`), or whose connection acquisition or
identity check timed out inside the replay window (`ErrReaderUnavailable` joined
with `context.DeadlineExceeded`, through `privateFailure`), reached API and MCP clients as
HTTP 500 with the Go error text. The checkpoint middleware already answered 503
for a failed checkpoint step, so the same transient condition had two contracts.

## Root cause

Observation, from source at the base of this change: nothing under
`go/internal/query` referenced the reader sentinels. `querycontract`'s shared
graph-read helper mapped only `ErrGraphUnavailable` (503) and
`ErrGraphReadDeadline` (504). The dead-code handlers (`deadcode/results.go`,
`cross_repo.go`, `investigation.go`) called the helper after the candidate scan
but wrote `err.Error()` into a 500 for the cross-repo consumer-evidence read and
the investigation coverage read, and `iac/handler.go` (`handleDeadIaC`) never
called it. The regression tests below reproduced each as a 500 whose body carried
`PostgreSQL reader has not reached writer checkpoint` or
`PostgreSQL reader connection unavailable`, plus a 400 with the same text from
the dead-IaC selector read.

Not established: that the QA-cluster 500s in the issue were replay stalls. The
issue records the link as a hypothesis; the response contract is a defect either
way.

## Change

- `storage/postgres/db` holds the shared sentinels and the retry hint;
  `runtime/postgres` re-exports the same values, so `errors.Is` matches through
  `privateFailure` without the query layer importing the runtime package.
- `querycontract.WriteGraphReadError` / `GraphReadErrorEnvelope` map a reader
  timeout to 503 `backend_unavailable` with a fixed message and
  `details.retry_after_seconds`. A timeout is `ErrReaderStale` (always a replay
  deadline), or `ErrReaderUnavailable` that also satisfies
  `errors.Is(err, context.DeadlineExceeded)`: the pool wait, the connection dial
  (pgx v5 returns an error wrapping the context error when the dial hits the
  replay-window deadline), or the identity check (`runtime/postgres/reader.go`).
  The check runs before the deadline sentinel because both wrap
  `context.DeadlineExceeded`; `ClassifyBoundedGraphReadError` uses the same
  `isReaderFenceError` predicate, so the classifier and the mapper agree and
  leave these untouched. `Retry-After: 2` (a fixed constant, no clock read) is
  set only for the transient verdicts: `GraphReadErrorEnvelope` marks the
  envelope it returns for a 503 verdict (graph unavailable, stale or timed-out
  reader) with an unexported flag and `WriteErrorEnvelope` honors only a marked
  envelope, so every seam that writes a `GraphReadErrorEnvelope` result keeps
  the header; `WithCheckpoint` sets it for its own 503. The generic
  `WriteErrorEnvelope` no longer sets it for an arbitrary 503
  `backend_unavailable`. The pre-existing graph-unavailable 503 is transient and
  keeps the header.
- Review fix (blocking P2): an earlier revision of this change set `Retry-After`
  in `WriteErrorEnvelope` on every 503 `backend_unavailable`, which also reached
  `codequery/route_handlers.go` answering "route-to-caller tracing requires a
  configured graph backend" when `h.Neo4j == nil`, a permanent configuration
  state. The header moved to the marked graph-read envelopes. Other 503 writers
  were decided per site (below).
- A bare `ErrReaderUnavailable` is NOT retryable. `runtime/postgres/reader.go`
  joins that sentinel onto every `reader.Conn` failure (authentication, TLS,
  connection refused, client `context.Canceled`), every non-topology identity
  query error (for example permission denied on `pg_control_system`), and every
  non-context replay-query error, so mapping the sentinel alone told permanent
  failures "retry shortly". They now fall through to the caller's own 500 with
  the fixed private text, no `Retry-After`, and no driver text.
  `ErrWrongTopology` and `ErrMissingCheckpoint` were never mapped and stay 500.
- The dead-code, cross-repo, investigate, and `iac/dead` handlers route every
  store read failure through the helper. MCP inherits it: the dispatcher sets
  `isError` for status >= 400 and forwards the envelope; a 500 reaches MCP as the
  plain HTTP 500 dispatch error with the fixed text.
- Residual: a driver timeout that is not a `context` error (an OS-level dial
  timeout surfaced as `os.ErrDeadlineExceeded` without a wrapped context error)
  would not satisfy `context.DeadlineExceeded` and so answers 500. The pool wait,
  the dial, the identity check, and the replay wait are all bounded by the
  replay-window context, so the measured pool-wait path does carry the context
  deadline (`TestReaderBorrowPoolWaitTimeoutIsRetryableDeadline`).
- Residual: `fenceCtx` in `runtime/postgres/reader.go` is
  `context.WithTimeout(ctx, replayTimeout)`, so it inherits the request's own
  deadline. When a parent or handler budget expires during borrow, identity, or
  replay, the response is now the retryable 503 where it previously answered 500
  on the dead-code and `iac/dead` routes (they never classified reader errors with
  `ClassifyBoundedGraphReadError`), or 504 on a route that did. This is
  defensible: the read did not fail on a bounded graph read, and a retry may land
  inside a fresh budget. Operators see it as a 503 with `outcome="deadline"` on
  `reader_borrow`, `reader_identity`, or `reader_replay`.

## Proof

Failing regression first, then green:

- `go test ./internal/query/querycontract/ -run 'ReaderFence|UnknownErrors|RetryHint|KeepsReaderVerdict'`
  failed before the mapping (helper returned false, classifier turned the stale
  error into a 504), passes after.
- `go test ./internal/query/iac/ -run 'ReaderFence|UnknownStoreErrors'` failed
  with 500 and 400 responses carrying the Go text at all five read sites, passes
  after.
- `go test ./internal/query/codequery/ -run 'ReaderFence|UnknownStoreErrors'`
  failed at the cross-repo evidence and investigation coverage sites (500 with Go
  text), passes after; the scan sites pass through the shared helper.
- `go test ./internal/mcp/ -run ReaderFence` asserts the `find_dead_code` and
  `find_dead_iac` tool results are `isError` with code `backend_unavailable`.
- An unrelated store error still answers 500 with no `Retry-After` (no
  over-mapping).
- Review fix (blocking P2, `Retry-After` scope): the regressions were written
  first and failed against cab9c190a with `Retry-After = "2"` on a 503 that is
  not a graph-read verdict:
  `go test ./internal/query/querycontract/ ./internal/query/codequery/ -run 'NoRetryAfter|KeepsRetryAfter|StaysWithoutRetryAfter'`
  exit 1 (`TestWriteErrorEnvelopeBackendUnavailableCarriesNoRetryAfter` on a
  generic `WriteErrorEnvelope` 503, and
  `TestRouteToCallerWithoutGraphBackendCarriesNoRetryAfter` through the real
  `CodeHandler.Mount` mux with `Neo4j == nil`). After the change the same command
  exits 0. `TestRouteToCallerGraphUnavailableKeepsRetryAfter` and
  `TestGraphUnavailableVerdictKeepsRetryAfterThroughWriteGraphReadError` keep
  the transient graph-unavailable 503 retryable, and the existing
  `TestGraphReadErrorEnvelopeSeamCarriesRetryHint` and the `WithCheckpoint` test
  keep the seam and checkpoint headers.

### Other 503 `backend_unavailable` writers (decided per site)

Re-derived with `rg -n 'WriteErrorEnvelope\(' go --glob '!*_test.go'` and
`rg -n 'StatusServiceUnavailable' go --glob '!*_test.go'`:

- Graph-read verdicts through `WriteGraphReadError` and
  `GraphReadErrorEnvelope` (including the service-story seam, which
  `serviceintelhttp` and `entity.GetServiceStory` write through the generic
  writer): transient, keep the header via the marker.
- `codequery/route_handlers.go` (`h.Neo4j == nil`): permanent configuration,
  no header (this fix).
- `WriteContractError` sites (secrets, observability coverage): never set the
  header; unaffected.
- `WriteError` and `http.Error` 503 sites (store not configured, status reader
  not configured, setup store, login, ask-not-ready): never set the header;
  unchanged.
- `entity/service_story_seam.go` `ErrContentSubstringIndexesNotReady` 503: not a
  graph-read verdict, so it carries no header, as before this change. Whether an
  index build is worth a retry hint is a separate contract question and is not
  decided here.
- `semanticsearch` `writeSemanticSearchError` 503: own writer, unchanged.
- `runtime/postgres` `WithCheckpoint`: transient (a failed checkpoint step),
  sets the header directly.
- Review fix (blocking P2): the non-transient cases were added first and failed
  against the prior commit with 503 where 500 was required
  (`TestWriteGraphReadErrorLeavesNonTransientReaderFailuresUnclaimed`,
  `TestClassifyBoundedGraphReadErrorDoesNotTreatNonTransientReaderAsFence`,
  `TestDeadCodeRoutesKeep500ForNonTransientReaderFailures`,
  `TestHandleDeadIaCKeeps500ForNonTransientReaderFailures`,
  `TestDispatchToolKeepsNonTransientReaderFailuresOutOfRetryableEnvelope`), each
  over a permission-denied identity error, a connection-refused error, and a
  client `context.Canceled`, all joined with `ErrReaderUnavailable` behind a
  private-text wrapper. They pass after the predicate narrowing, while the
  pool-wait (`ErrReaderUnavailable` + `DeadlineExceeded`) and stale cases stay
  503. `TestReaderBorrowPoolWaitTimeoutIsRetryableDeadline` drives a real
  saturated `database/sql` pool through `borrowFresh` and shows the production
  pool-wait error satisfies both `ErrReaderUnavailable` and
  `context.DeadlineExceeded` through `privateFailure`, and that a connect
  failure does not.

## Performance and concurrency

No-Regression Evidence: no SQL, Cypher, lock, claim, lease, queue, pool-size, or timeout change. The edit touches only error mapping. The two `errors.Is` checks and the header write run once per request that already failed, after the reader fence returned its error, and `ReaderRetryAfterSeconds` is a constant. The replay wait, the 2 s replay timeout, the pool, and the single-connection fence in `runtime/postgres` are untouched, so the accuracy, performance, and concurrency contracts of the read path are unchanged; the sentinel re-export (`ErrReaderStale = db.ErrReaderStale`) changes no behavior. Verified by `go test ./internal/runtime/postgres/ -count=1` (existing fence tests plus `TestReaderSentinelsAreTheSharedDBSentinels`) and `go test ./internal/query/... ./internal/mcp/... -count=1`. No timing was measured; none is claimed.

## Observability

Observability Evidence: no new metric. Fence outcomes are already observable in
`eshu_dp_postgres_reader_stage_duration_seconds` with `role="reader"`:
`stage="reader_replay"`, `outcome="deadline"` is a replica that missed the
checkpoint (`ErrReaderStale`); `stage="reader_borrow"`, `outcome="deadline"` is a
pool-wait or dial timeout (a `reader_identity` `deadline` is the same 503);
`reader_replay` `outcome="ok"` with a duration near the replay
window is a request that waited and succeeded. The same stages with
`outcome="error"` or `"canceled"` answer 500, not 503, and `outcome="error"` is
the operator signal for a permanent reader misconfiguration (credentials,
network, grants). `eshu_dp_postgres_reader_pool_waits_total`
and `eshu_dp_postgres_reader_pool_wait_duration_seconds` show pool pressure.
Documented in `docs/public/reference/telemetry/metrics.md` and
`docs/public/deployment/postgres-read-routing.md`.

No-Observability-Change: eshu_dp_postgres_reader_stage_duration_seconds, eshu_dp_postgres_reader_pool_waits_total, eshu_dp_postgres_reader_pool_wait_duration_seconds

## Not done

This change covers the dead-code (`/api/v0/code/dead-code`, `.../cross-repo`,
`.../investigate`) and `/api/v0/iac/dead` routes and the MCP tools that call them
(`find_dead_code`, `find_dead_iac`). Every other handler that answers a store
error with `StatusInternalServerError` and `err.Error()` is unchanged: a reader
timeout there still reaches the client as the same 500 as before. A handler that
already calls `WriteGraphReadError` for some of its reads gains the mapping on
those reads only; a 500 site in the same file that does not call it first is
still unmapped.

Re-derived with `rg -U 'StatusInternalServerError,\s*\n?\s*err\.Error\(\)|StatusInternalServerError,\s*[a-zA-Z.]*Sprintf\([^)]*err|WriteError\([^)]*StatusInternalServerError[^)]*err\.Error' go/internal/query`
(non-test files, excluding `codequery/deadcode/results.go` and `iac/handler.go`
which this change covers): 260 sites in 23 directories, with the
per-file site count after each name. The count is an inventory of the textual
pattern, not a per-site proof that each is a Postgres read; the pattern misses
a 500 written through a differently shaped call.

- `go/internal/query` (43 sites): `admission_decisions.go` 2, `evidence_bundle_live.go` 2, `evidence_citation.go` 2, `evidence.go` 1, `graph_entity_inventory.go` 3, `images.go` 1, `infra_ecosystem_overview.go` 1, `infra_graph_summary_packet.go` 2, `infra_relationship_filter.go` 1, `infra_resource_aggregates_handler.go` 2, `infra.go` 1, `investigation_packet_api_deployable.go` 3, `investigation_packet_api_drift.go` 2, `investigation_packet_supply_chain.go` 2, `operator_control_plane.go` 1, `relationships_catalog.go` 2, `status_answer_narration.go` 1, `status_collector_readiness.go` 1, `status_freshness_causality.go` 1, `status_governance.go` 2, `status_hosted_readiness.go` 1, `status_operations.go` 2, `status_scoped.go` 1, `status_semantic_extraction.go` 1, `status.go` 4, `tag_history.go` 1
- `go/internal/query/admin` (19 sites): `changedsincepoisonedlinks.go` 1, `deadletters.go` 1, `facts.go` 6, `generations.go` 3, `handler.go` 2, `inputinvalid.go` 1, `replay_explicit.go` 2, `replay.go` 3
- `go/internal/query/cicd` (3 sites): `handler.go` 1, `run_correlation_aggregates_handler.go` 2
- `go/internal/query/codeowners` (2 sites): `ownership.go` 2
- `go/internal/query/codequery` (37 sites): `callers.go` 2, `compare_paths.go` 1, `complexity_queries.go` 3, `divergence_investigate.go` 4, `divergence_report.go` 1, `divergence.go` 1, `flow.go` 1, `graph_metrics.go` 1, `handler.go` 3, `import_dependencies.go` 1, `inspection.go` 1, `relationship_handlers.go` 7, `route_handlers.go` 3, `security_secrets.go` 1, `story_handlers.go` 4, `structural_inventory.go` 1, `symbol.go` 1, `topic.go` 1
- `go/internal/query/compare` (4 sites): `handler.go` 4
- `go/internal/query/contentread` (5 sites): `content_handler.go` 5
- `go/internal/query/dependency` (1 sites): `handler.go` 1
- `go/internal/query/entity` (19 sites): `content_types.go` 2, `context_handler.go` 3, `handler.go` 5, `resolve_workload.go` 1, `service_context_handler.go` 2, `service_investigation.go` 2, `workload_handlers.go` 4
- `go/internal/query/freshness` (3 sites): `changed_since.go` 1, `generations.go` 1, `service_changed_since.go` 1
- `go/internal/query/iac` (17 sites): `aws_runtime_drift.go` 2, `import_plan.go` 2, `management_surface.go` 2, `management.go` 2, `replatforming_ownership_handler.go` 2, `replatforming_plan_handler.go` 2, `replatforming_rollups_handler.go` 2, `resources.go` 3
- `go/internal/query/impact` (25 sites): `blast_radius.go` 1, `change_surface_investigation.go` 2, `change_surface_legacy.go` 1, `contract.go` 1, `deployment_config_influence.go` 2, `entity_map.go` 2, `exposure_path.go` 1, `handler.go` 6, `resource_investigation.go` 2, `trace_deployment.go` 7
- `go/internal/query/incident` (1 sites): `handler.go` 1
- `go/internal/query/kubernetes` (1 sites): `handler.go` 1
- `go/internal/query/metrics` (1 sites): `handler.go` 1
- `go/internal/query/observability/coverage` (1 sites): `handler.go` 1
- `go/internal/query/package/registry` (14 sites): `aggregates_handler.go` 2, `correlation_handler.go` 1, `dependencies_handler.go` 1, `dependency_chains_handler.go` 1, `handler.go` 3, `scoped_gates.go` 6
- `go/internal/query/repository` (35 sites): `branches.go` 2, `catalog.go` 3, `content.go` 3, `context.go` 3, `freshness.go` 2, `handler.go` 11, `language_inventory.go` 3, `selectors.go` 2, `tree.go` 6
- `go/internal/query/secrets` (6 sites): `handler.go` 1, `posture_handlers.go` 3, `summary.go` 2
- `go/internal/query/service` (1 sites): `catalog.go` 1
- `go/internal/query/supply/chain` (19 sites): `catalog_handler.go` 1, `container_image_identity_aggregates_handler.go` 2, `container_images.go` 1, `evidence_handler.go` 1, `explain_handler.go` 1, `findings_aggregates_handler.go` 2, `findings_handler.go` 1, `investigation_packet_api.go` 1, `repository_selector.go` 2, `sbom_attachments.go` 1, `sbom_attestation_attachment_aggregates_handler.go` 2, `security_alert_reconciliation_aggregates_handler.go` 2, `security_alerts.go` 1, `vulnerability_detail_handler.go` 1
- `go/internal/query/terraform/drift` (2 sites): `handler.go` 2
- `go/internal/query/workitem` (1 sites): `handler.go` 1

The live Postgres replica behavior and the QA-cluster symptom were not
reproduced. Routing these handlers through the helper is follow-up work.
