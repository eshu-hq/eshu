# Service Documentation Evidence Family And Source ACL State Projection (#1988, #2163, #1901)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Service documentation evidence family (#1988)

The docs evidence family extends the same service materialization lineage (#1943)
without a new table, index, or reducer domain. Unlike the deployment/dependencies
families (resolved relationships, repo-keyed) and the runtime family
(graph-materialized instances, repo-keyed), the docs family is sourced from
`fact_records` and keyed by SERVICE id: documentation facts link to a service
through their target refs (`candidate_refs` / `evidence_refs` /
`linked_entities`), not through a repository generation. On each
`service_catalog_correlation` intent, `ServiceCatalogCorrelationHandler` (when both
`MaterializationWriter` and `DocumentationEvidenceLoader` are wired) loads the
correlated services' referencing documentation facts in ONE bounded
`GetDocumentationEvidenceForServices` call. Each documentation fact
(`documentation_entity_mention`, `documentation_claim_candidate`,
`semantic.documentation_observation`) becomes one docs-family
`service_evidence_snapshots` row in the same service generation as ownership,
deployment, runtime, and dependencies.

The row identity is `ServiceDocumentationEvidenceKey(service_id, identity)` =
`docs:<service_id>:<source_system>:<source_record_id>:<document_id>`, where the
identity is the documentation fact's durable EXTERNAL identity: `source_system`
and `source_record_id` are durable `fact_records` columns (the collector emits a
durable section/document ref into `source_record_id`, e.g. the documentation
section id), and `document_id` is a durable payload field. The fact's `fact_id`
is deliberately NOT used: the documentation collectors digest the `generation_id`
into `FactID`, so keying on it would assign every fact a new key each generation
and report 100% false churn; the read model treats `generation_id` as a scope
constraint only, never as identity. The external-identity tuple keeps the same
fact stable across generations so the FULL OUTER JOIN diff classifies it
`unchanged`. The production loader
(`postgres.ServiceDocumentationEvidenceLoader`) reads the active-generation,
non-tombstone documentation facts that reference the service through the same
JSONB target-ref containment shapes the query-layer documentation read model uses,
so the reducer load and the read surface agree on what "references this service"
means.

### Source ACL state projection (#2163, #1901 vertical)

Each docs-family row also carries the bounded `source_acl_state`
(`allowed|denied|partial|missing|stale`) the collector emits on the documentation
fact's `acl_summary` (`facts.DocumentationACLSummary.SourceACLState`, #2162). The
loader reads it verbatim from `fact.payload->'acl_summary'->>'source_acl_state'`,
`COALESCE`d to the empty string, and `serviceDocumentationEvidencePayload` projects
it into the snapshot payload only when it is one of the bounded states
(`projectedSourceACLState` validates against `facts.ValidSourceACLState`). It is an
access-posture axis kept DISTINCT from freshness (#2138 truth-label taxonomy): the
reducer never folds it into freshness, never upgrades a denied/partial/missing/stale
observation to allowed, and never synthesizes a default the collector did not
assert. When the fact carries no bounded ACL signal the field is omitted from the
payload entirely (absence means "no ACL claim", fail closed). Because it lives in
the hashed observable payload, a changed bounded ACL state flips the row to
`updated` while an unobserved or unchanged state cannot churn the generation. The
documentation evidence fact kinds (`documentation_entity_mention`,
`documentation_claim_candidate`, `semantic.documentation_observation`) do not yet
carry an `acl_summary`, so today the projected payload omits the field for them; the
projection is additive and forward-compatible for when those facts carry the bounded
state. Choosing a conservative default for unobserved sources, and disclosing the
field on the read surface, are deferred to the query child (#2164) and security
review; this reducer slice only carries the collector value verbatim and never
surfaces it.

No-Regression Evidence: `go test ./internal/reducer -run 'TestDocsEvidence' -count=1`
proves the docs payload carries every bounded `source_acl_state` verbatim, omits an
unobserved state, drops a non-bounded value rather than projecting it, and that a
changed bounded state flips the row hash while an identical state stays unchanged
(anti-churn); `go test ./internal/storage/postgres -run
'TestServiceDocumentationEvidenceLoader' -count=1` proves the loader reads
`source_acl_state` from `acl_summary` and scans the empty string when the fact has no
ACL summary. The projection adds one COALESCE'd JSONB text read to the same bounded
per-service documentation query (no new index, scan, join, or row), so it is a
constant-factor addition to an already-bounded read with no new generation, lease,
worker, batch, or queue domain; the row hash is computed by the same
`ServiceEvidencePayloadHash` the family already uses. `go test ./internal/reducer
-race -run 'TestDocsEvidence|TestServiceMaterializationDocsChangeFlipsGeneration|TestServiceCatalogHandlerCommitsDocsFamilyWhenWired'
-count=1` passes under the race detector, confirming the additive field introduces
no shared-state hazard in the existing worker/lease materialization path.

Observability Evidence: No-Observability-Change: this slice adds no metric
instrument, label, span, queue domain, lease, or runtime knob. The ACL field rides
the existing docs-family `service_evidence_snapshots` payload inside the
`service_catalog_correlation` reducer execution span/counter and the same
materialization commit path Stage-1 already instruments; operators observe it
through the existing reducer run spans, execution counters, `fact_work_items`
status/failure fields, and the durable `service_materialization_generations`/
`service_evidence_snapshots` rows. No read surface exposes the value yet (deferred
to #2164), so no new operator-facing signal is required.

No-Regression Evidence: `go test ./internal/reducer -run
'ServiceDocumentation|BuildServiceDocumentation|ServiceMaterialization|ServiceCatalogHandler'
-count=1` proves docs rows carry generation-stable external-identity keys (no
embedded `fact_id`/`generation_id`), a changed payload hash flips the generation
while an identical re-materialization is a no-op, dropped facts are tombstoned,
records without a complete durable identity are dropped rather than keyed on an
empty identity, and the docs family is purely additive (no loader leaves the
generation without docs rows and ownership/deployment/runtime/dependencies tests
stay green). `go test ./internal/storage/postgres -run
'TestComputeServiceChangedSinceDelta|TestServiceDocumentationEvidenceLoader'
-count=1` proves the family-generic delta SQL (grouped by `evidence_family`)
classifies docs added/updated/unchanged/retired/superseded with bounded ordered
samples, reports the docs family unavailable (never zero-delta) when no active
generation exists, reports zero deltas for a non-docs fixture, and the loader
scopes by service, gates on the active generation and non-tombstone documentation
facts, and projects only durable identity. The docs rows reuse the existing
`service_evidence_snapshots_diff_idx` (`generation_id`, `evidence_family`,
`service_evidence_key`) the Stage-1 delta query already drives; the
documentation fact read is bounded per service over the documentation fact kinds
and the active-generation join, so no new index or schema migration is added.
Live-Postgres SQL is proven by the same fake `Queryer`/`Rows` harness Stage-1
uses; the live SQL gate is the Postgres integration suite in CI.

Observability Evidence: this slice adds no new metric instrument, label, queue
domain, lease, or runtime knob. The docs family lands inside the existing
`service_catalog_correlation` reducer execution span/counter and the same service
materialization commit path Stage-1 already instruments; operators diagnose it
through reducer run spans, execution counters, `fact_work_items` status/failure
fields, the durable `service_materialization_generations`/
`service_evidence_snapshots` rows, and the `get_service_changed_since`
API/MCP/CLI read surface that now reports the docs family alongside ownership,
deployment, runtime, and dependencies.
