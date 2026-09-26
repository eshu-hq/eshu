# Service Catalog Correlation Waits For The Relationship Corpus Fence

Issue #7258. `ServiceCatalogCorrelationHandler` fed the service deployment and
dependencies evidence families from the unfenced by-repos resolved read
(`listResolvedByReposSQL`, filtered on `g.status = 'active'`). While a foreign
scope's relationship generation is retired or pending, that read omits the
scope's rows (the "Negative control" in
[6740-corpus-fence-snapshot.md](6740-corpus-fence-snapshot.md)). The handler
committed a service materialization generation from that partial set,
superseding the prior one, and `GET /api/v0/freshness/services/changed-since`
reported the missing rows as removed. The only producer of
`service_catalog_correlation` intents
(`go/internal/projector/service/catalog/correlation_intents.go`) never reopens
them when the foreign scope completes.

## Fix

The handler field now requires `servicecatalog.CorpusFencedResolvedRelationshipLoader`,
a local structural twin of the root interface, so production cannot wire an
unfenced loader. `Handle` runs the fused
`GetResolvedRelationshipsForReposWithCorpusFence` read before
`WriteServiceCatalogCorrelations` and before any service generation write. A
`complete=false` verdict returns a retryable error classed
`service_catalog_correlation_resolution_not_ready`; nothing is written, so a
retry does not re-write correlation facts. A read error stays an ordinary
error. No repository among the decisions, or no materialization writer, means
no read and no fence, as before.

## Proof

Root-Cause Evidence: the #7258 regression
`TestServiceCatalogHandlerDefersWhileRelationshipCorpusFenceOpen` failed on
the pre-fix handler with "Handle() error = nil ... the handler materialized 1
service write(s) with 1 deployment row(s) from a partial resolved set". The
live negative control
`TestServiceCatalogCorpusFenceUnfencedReadReportsSpuriousRemovalLive` wires
the pre-fix unfenced read against real Postgres 18 and reproduces the harm:
changed-since reports deployment `superseded=1 unchanged=1` after the foreign
scope's generation goes pending, with no change to the service.

Live Postgres (`postgres:18-alpine`, isolated schema per test, real
`RelationshipStore` and `PostgresServiceMaterializationWriter`):
`TestServiceCatalogCorpusFenceDefersAndPreservesEvidenceLive` shows the
foreign generation going pending makes the handler defer with the new class,
the correlation writer is not called, and the service generation table still
holds only the first active generation. After re-activation plus one
legitimate own-scope row, the retry commits a new generation whose
changed-since deployment delta is `added=1 unchanged=2 retired=0
superseded=0`.

Hermetic proof: on the complete path the handler output (correlation write,
materialization writes, result) was byte-identical JSON before and after the
change for the same full row set. Unit tests cover deferral with zero writes,
no repository, nil materialization writer, read error, and a retry after
deferral matching a clean first run.

No graph writes: the handler writes Postgres correlation facts and service
materialization rows only; no Cypher or graph edge is involved.

No-Regression Evidence: the complete path replaces one unfenced by-repos
statement with one fused statement. The #6740 plans (laptop Postgres shim,
5000 active scopes, 200000 resolved rows, 10 repository ids, warm cache)
measured the by-repos read alone at 1.473 ms / 362 shared buffers and the
fused statement at 4.009 ms / 634 buffers when complete, 3.568 ms / 597 when
deferring. That is about 2.5 ms more per service catalog correlation intent,
which runs once per catalog generation and is not on a per-fact hot path.
Statement count does not change. A deferral runs the one fused read and no
writes, where the pre-fix handler ran the read plus the correlation write
and service generation commit. Worker count, lease, and batch settings are
unchanged.

No-Observability-Change: no new metric, span, or log key. A deferral surfaces
as the durable `failure_class` `service_catalog_correlation_resolution_not_ready`
on the work item, enrolled in `nonCountingReducerRetryFailureClasses` so it
does not spend the retry budget, and visible through the same queue signals
as the sibling deferrals (`eshu_dp_queue_claim_duration_seconds`,
`eshu_dp_reducer_queue_wait_seconds`, `eshu_dp_queue_depth`, and
`fact_work_items` status and failure class). The fused read is timed by
`eshu_dp_postgres_query_duration_seconds` as before. The telemetry-coverage
readiness-gates row names the new class.
