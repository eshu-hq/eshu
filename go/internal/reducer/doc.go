// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reducer owns Eshu's cross-domain materialization, shared projection,
// queued repair, and reducer-owned fact publication.
//
// Reducer handlers admit candidates from committed facts, build canonical graph
// rows or reducer fact rows, publish graph-readiness phases, and preserve
// idempotency across retries and replays. They do not call graph drivers
// directly; canonical graph writes go through storage/cypher, and durable fact
// writes go through narrow writer interfaces wired by cmd/reducer.
// Repo-wide shared-projection refresh fences are generation-local: an exact
// same-generation retry reuses completed deterministic intent IDs, while a
// later generation must complete its own refresh before its edge rows write.
// SQL relationship materialization resolves bounded parser metadata within one
// repository and emits exact-label read, foreign-key, routine-write, trigger,
// index, migration, column, and embedded-query edges; missing or ambiguous
// targets are counted and skipped rather than guessed.
//
// Changes in this package must preserve the evidence path from raw facts to
// admitted candidate, projected row, graph or fact write, and API/MCP query
// truth. Queue ordering, generation supersession, phase publication, repair
// flows, shared projection readiness, bounded generation-retention cleanup,
// bounded graph orphan cleanup, and truth-emitting domain registration are
// package-level contracts. Shared AdmissionDecisionWriter hooks, when wired,
// persist explainable admitted, rejected, ambiguous, stale, missing-evidence,
// unsupported, permission-hidden, and unsafe outcomes for mapped correlation
// domains after their existing canonical writers succeed; they do not change
// graph eligibility or fabricate canonical rows for provenance-only evidence.
// GenerationRetentionRunner prunes only superseded source-generation history in
// bounded Postgres transactions; it never substitutes for relationship
// retraction or graph orphan cleanup. GraphOrphanSweepRunner marks and deletes
// only aged zero-relationship graph nodes from a closed label set; it is a
// safety cleanup after owned retractions, not a replacement for relationship or
// source-local canonical cleanup. SearchVectorBuildRunner builds only derived
// vector rows for active curated search documents and never writes graph truth.
// SupplyChainImpactHandler also evaluates
// vulnerability.suppression facts and writes the resulting VEX or operator
// policy decision onto every impact finding; provider dismissals stay
// evidence and never auto-hide findings. Its reducer result exposes
// phase-level sub-durations, including provider security-alert scoping, so
// operators can separate fact-load, reachability, in-memory admission, and
// durable-write cost from the aggregate handler duration; its bounded diagnostic
// signals also distinguish pre-load counts from post-scope evidence counts. The
// handler also computes an advisory-only safe-upgrade remediation per finding
// for ecosystems whose version and manifest semantics are represented in reducer
// matchers: it never auto-opens pull requests, and unsupported remediation
// remains explicit. The remediation block names the current version,
// vulnerable range, fixed-version source, match reason, first patched version,
// manifest-allows-fix decision, direct/transitive designation, parent package
// required for transitive upgrades, and an exact/partial/unknown confidence
// label so API and MCP callers can explain the upgrade path. Vendor-proven RPM,
// Debian/dpkg, and Alpine/APK remediation stays limited to parseable installed
// versions and single source-attributed fixed branches; missing provenance or
// ambiguous branches remain explicit missing evidence. Supply-chain impact
// version matching is ecosystem-aware for npm, Cargo, Pub, Swift, NuGet, Maven,
// and PyPI PEP 440 exact-version evidence; unsupported or malformed ranges fail
// closed with explicit missing evidence. Exact repository-scoped
// service-catalog correlation facts stay attached to the supply-chain evidence
// path, but they do not create service or workload ids unless the catalog row
// carries those anchors; missing anchors are reported as service/workload
// catalog anchor missing rather than treating the catalog correlation as
// absent. Security-alert reconciliation facts are keyed by provider alert
// identity, package identity, advisory ids, and provider evidence scope so
// provider-only placeholders are replaced by later matched or stale rows while
// preserving reason and evidence references for audit. They also carry the
// Eshu-owned observed package version and dependency evidence gaps used for
// reconciliation without copying provider alert fields into observed-version
// truth. SBOM attachment decisions preserve aggregate warning occurrence counts
// from source warning facts while keeping warning summaries as bounded previews;
// scanner workers still emit source facts only and do not own attachment truth.
// S3 internet exposure materialization writes reducer-owned exposed /
// not_exposed / unknown posture properties onto existing S3 CloudResource nodes
// only, preserving unknown evidence as unknown rather than safe.
// EC2 internet exposure materialization does the same for existing EC2
// CloudResource nodes using EC2 posture, ENI topology, and security-group rule
// evidence without storing raw public IP addresses or treating missing topology
// as safe.
// Secrets/IAM trust-chain materialization consumes redaction-safe AWS IAM, GCP
// IAM, Kubernetes ServiceAccount/workload, and Vault anchors. The GCP path
// admits GKE Workload Identity chains only when Kubernetes annotation evidence,
// GCP ServiceAccount trust, GCP principal evidence, and a Secret Manager
// version-access grant all agree; metadata-only Secret Manager roles remain
// posture evidence and never become exact secret access paths.
// Azure relationship materialization admits fixture/offline azure_cloud_resource
// facts into CloudResource nodes only so managed_by azure_cloud_relationship
// facts can readiness-gate on those endpoints, then writes edges only when both
// normalized ARM IDs resolve exactly in the same source generation.
// Code function summary materialization persists generation-independent
// value-flow summaries, param sources, and FunctionID-to-graph-uid mappings.
// ValueFlowProgramAssemblyRunner can assemble bounded Programs from active
// CALLS, persisted summaries, and durable param-source rows without solving or
// writing graph evidence. Shell execution materialization consumes parser
// command-call facts and projects Function-[:EXECUTES_SHELL]->ShellCommand using
// structural metadata only; command text and arguments never enter reducer
// payloads or graph properties. The fixpoint path loads graph-backed cloud sink
// targets from the closed exposure catalog, then runs the cross-repo fixpoint
// projection through a distinct
// reducer/code-interproc-fixpoint TAINT_FLOWS_TO evidence source so direct
// code_interproc_evidence rows remain isolated. Cloud action permission targets
// join only through an exact single RUNS_IN workload fan-out plus
// WorkloadInstance USES CloudResource principal and matching CAN_PERFORM action;
// ambiguous runtime identity stays empty. Cloud sink bridge edges are attached
// only to observed parameter ports for that FunctionID; a graph edge without
// parameter evidence stays visible as no value-flow finding rather than
// fabricating precision.
// The fixpoint solve caches weakly-connected value-flow components inside the
// reducer process, keyed by component membership, durable summary content
// versions, and external source/sink inputs. When one function summary version
// changes, only the component that can carry taint from that function is
// recomputed; unrelated components reuse their cached findings before the final
// global sort/cap and existing global fixpoint evidence rewrite.
//
// Performance Evidence: synthetic local benchmark on 100 independent value-flow
// components with 100-hop chains, changing one function summary version after
// warming the cache:
// `go test ./internal/reducer -run '^$' -bench 'BenchmarkValueFlowFixpoint(Full|Incremental)' -benchmem -count=3`
// reported full recompute at 7.67-7.71 ms/op with 15.22 MB/op and about 33.7k
// allocs/op, while the cached incremental path reported 7.31-7.35 ms/op with
// 11.36 MB/op and about 5.2k allocs/op. This is a deterministic local corpus,
// not a full remote corpus proof.
// No-Regression Evidence: `go test ./internal/reducer -run
// 'TestValueFlowFixpoint(Cache|EvidenceLoader|EvidenceProjector)' -count=1`
// proves component-level cache reuse, full-solve parity, cloud sink behavior,
// unresolved endpoint behavior, and unchanged global fixpoint write semantics.
// `go test ./internal/reducer -count=1` passed for the reducer package.
// Observability Evidence: the existing `value-flow fixpoint evidence loaded`
// structured log now includes `fixpoint_component_count`,
// `fixpoint_recomputed_components`, and `fixpoint_reused_components` alongside
// the existing summary/source/sink/finding/overflow/unresolved counts. The
// change adds no metric label or high-cardinality metric.
//
// No-Regression Evidence: issue #3249 partitions durable summary/source/sink
// snapshots before Program assembly and persists solved weak-component fixpoint
// results behind the same component-content cache key, so a reducer restart or
// second replica can reuse unchanged components without reassembling or solving
// them. `go test ./internal/reducer -run
// 'TestValueFlowFixpoint(Snapshot|Durable|Cache|EvidenceLoader)' -count=1`
// proves restart reuse, changed-summary-version invalidation, directed
// edge-shape invalidation, assembly limited to the changed component, full-solve
// parity, cloud sink behavior, unresolved endpoint behavior, and unchanged
// fixpoint projection semantics. `go test
// ./internal/storage/postgres -run
// 'TestValueFlowFixpointComponent|TestBootstrapDefinitionsIncludeValueFlowFixpointComponents'
// -count=1` proves the Postgres cache table, idempotent upsert, bounded
// key-list load, JSON result round trip, and bootstrap registration. `go test
// ./cmd/reducer -run TestNewValueFlowFixpointProjectorWiresCloudSinkGraphLoader
// -count=1` proves production reducer wiring passes the durable component store
// into the fixpoint loader.
// Performance Evidence: `go test ./internal/reducer -run '^$' -bench
// 'BenchmarkValueFlow(Snapshot|Fixpoint)' -benchmem -count=3` on a synthetic
// 100-component x 100-hop corpus reported full snapshot assembly+solve at
// 20.2-24.1 ms/op with about 92.5 MB/op and 91.8k-92.0k allocs/op, while the
// durable-restart cached snapshot path reported 12.6-13.9 ms/op with about
// 18.2 MB/op and 7.9k-8.0k allocs/op. The benchmark asserts exactly one
// assembled and recomputed component plus 99 durable reused components after a
// single function summary version change.
// Observability Evidence: #3249 extends the existing `value-flow fixpoint
// evidence loaded` structured log with `fixpoint_assembled_components` and
// `fixpoint_durable_reused_components` beside the existing component,
// recompute, reuse, summary, source, sink, finding, overflow, and
// unresolved-endpoint counts. It adds no route, graph query shape, graph write
// route, queue domain, worker, lease, runtime knob, metric instrument, or metric
// label. Operators diagnose durable cache behavior through that log plus
// existing reducer execution spans/counters and instrumented Postgres query/exec
// spans for the component store.
//
// No-Regression Evidence: issue #2967 adds one bounded graph read anchored on
// Function.uid values loaded from the durable FunctionID map and filtered by the
// graph-backed exposure sink relationship allowlist. Issue #2969 adds one
// additional Function.uid-bounded read for correlated INVOKES_CLOUD_ACTION to
// CAN_PERFORM permission paths, guarded by exact workload fan-out. The fixpoint
// graph writer shape is unchanged: cloud-backed findings still project as
// reducer/code-interproc-fixpoint TAINT_FLOWS_TO evidence between existing
// Function nodes, with rel.cloud=true and no promotion to canonical truth.
// No-Observability-Change: operators diagnose this path through the existing
// value-flow fixpoint load log fields, reducer execution spans/counters, graph
// query instrumentation, and CodeInterprocEvidence writer statement summaries;
// no worker, queue domain, metric instrument, metric label, runtime knob, or
// graph write route changes.
// Code-call resolution dispatches language-specific resolver branches through a
// registry seam before and after the weak repository-wide fallback; the generic
// resolver still preserves ambiguity and never promotes a language branch into a
// guessed canonical edge without explicit evidence.
// IncidentRoutingMaterializationHandler writes exact PagerDuty
// IncidentRoutingEvidence graph rows only for safe declared/applied/live
// convergence or live-only no-IaC routing evidence; unsafe routing outcomes
// remain provenance-only.
// PostgresServiceMaterializationWriter commits the additive per-service
// evidence generation lineage (#1943, #1985, #1986, #1987, #1988, #1989) the service-scope
// changed-since delta diffs: one active service_materialization_generations row
// per service_id (conflict key service_id, single active row enforced by a
// partial unique index) plus generation-stable service_evidence_snapshots rows
// keyed by a generation-independent service_evidence_key. Each generation
// snapshots every evidence family the writer knows: ownership
// (ServiceEvidenceFamilyOwnership, keyed by ServiceOwnershipEvidenceKey),
// deployment (ServiceEvidenceFamilyDeployment, keyed by ServiceDeploymentEvidenceKey
// over the resolved deployment relationship's generation-independent natural
// key), runtime (ServiceEvidenceFamilyRuntime, keyed by ServiceRuntimeEvidenceKey
// over the durable platform/environment/workload identity of each materialized
// runtime instance), and dependencies (ServiceEvidenceFamilyDependencies, keyed
// by ServiceDependencyEvidenceKey over the resolved dependency relationship's
// generation-independent natural key — DEPENDS_ON / USES_MODULE /
// READS_CONFIG_FROM, the complement of the deployment family from the same
// resolved_relationships source), and docs (ServiceEvidenceFamilyDocs, keyed by
// ServiceDocumentationEvidenceKey over the durable external identity
// source_system/source_record_id/document_id of each documentation fact that
// references the service, read from fact_records and keyed by service id rather
// than repository id; each docs row also carries the collector-observed bounded
// source_acl_state — allowed|denied|partial|missing|stale — verbatim in its
// payload as an access-posture axis distinct from freshness, omitted when the
// fact asserts no bounded ACL claim and never upgraded to allowed, #2163), and
// incidents (ServiceEvidenceFamilyIncidents, keyed by
// ServiceIncidentEvidenceKey over the durable routing identity
// provider/provider_incident_id/slot/evidence_kind/evidence_id of each exact
// PagerDuty incident-routing evidence row, where evidence_id is the source fact's
// generation-independent StableFactKey or content-entity id, never the
// generation-bearing FactID). The generation id is deterministic in the full
// evidence set, so an identical re-materialization is a no-op and a change in any
// family flips the generation; a dropped evidence row is tombstoned, never
// silently absent. It is wired into ServiceCatalogCorrelationHandler as an
// optional MaterializationWriter (with an optional DeploymentRelationshipLoader
// feeding both the deployment and dependencies families from one bounded load, an
// optional RuntimeInstanceLoader for the runtime family, an optional
// DocumentationEvidenceLoader for the docs family, and an optional
// IncidentEvidenceLoader for the incidents family) so the existing
// reducer_service_catalog_correlation fact contract is unchanged.
// CodeValueFlowStaleCleanupRunner scans active repository generations beside
// the normal reducer queue and retracts reducer-owned value-flow evidence from
// older generations only; its graph deletes are bounded and keyed by scope,
// evidence source, and generation inequality so current evidence survives
// concurrent materialization and gate-disabled refreshes.
//
// No-Regression Evidence: code-call language resolver registration preserves
// the previous Go branch order and resolution methods while allowing a new
// language resolver to run before `repo_unique_name` without editing the generic
// resolver. It changes no graph query, queue, worker, lease, batch, runtime knob,
// or storage contract.
//
// No-Observability-Change: resolver dispatch still emits existing durable
// code-call intent rows and the existing code-call materialization completion
// logs; no metric, span, status field, route, or log contract changes.
//
// recordQuarantinedFacts (factschema_decode.go) also best-effort persists each
// quarantined input_invalid fact to the durable reducer_input_invalid_facts
// read surface (issue #4630) through an optional QuarantinedFactWriter
// (quarantine_writer.go). Service stashes the writer on the execution context
// once per claimed intent via WithQuarantineWriter, so every domain handler's
// existing recordQuarantinedFacts call reaches it without a per-handler field.
// The write is batched (one round trip per intent), idempotent under
// reduction replay (a natural-key ON CONFLICT DO NOTHING upsert), and strictly
// best-effort: a durable-write failure is logged and counted but never fails
// the owning intent, since the fact is already correctly quarantined via the
// existing counter and structured log regardless of this write's outcome.
package reducer
