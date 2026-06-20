// Package status owns the shared reporting shape for Eshu pipeline state,
// backlog, generation lifecycle, and request-lifecycle health.
//
// Types in this package project raw runtime counts and lifecycle events
// into operator-facing reports consumed by the CLI, HTTP admin surfaces,
// and runtime status views. Keep these surfaces aligned: operators should
// not need a different mental model for each Eshu service. JSON shapes here
// are part of the operator contract and must change in lockstep with the CLI
// reference and runtime admin docs. The health projection treats reducer-owned
// shared projection backlog as unfinished graph-visible work, while lease-only
// shared-projection activity remains observable without blocking healthy, so
// code graph and dead-code queries do not look ready while accepted edges are
// still being written. The degraded health state weighs workflow-coordinator
// failures within CoordinatorRecentFailures (a bounded recent window) rather
// than cumulative all-time counts, so a recovered stack reports healthy again
// instead of staying degraded until aged failure rows are pruned; cumulative
// counts remain in the report as informational detail. The TerraformStateReport
// section, surfaced under Report.TerraformState, exposes per-locator state
// serial advance, safe source handles, and recent warning_fact rows grouped by
// warning_kind with severity/actionability classification so operators can
// separate blocking missing-state evidence from accepted parser guardrails.
// RegistryCollectorSnapshot rows expose aggregate OCI and package-registry
// runtime liveness, bounded failure classes, and package-registry metadata
// target counts without registry object names, package names, or credentials.
// CollectorRuntimeStatus rows derive a unified collector inventory from
// workflow coordinator registrations, durable direct status evidence, and
// active persisted source or reducer fact evidence, including Git repository
// ingestion facts, so coordinator-managed, direct-mode, disabled, and
// unregistered collectors are visible in one operator view.
// CollectorPromotionProof rows (CollectorPromotionProofs over a
// CollectorCatalog) project that runtime evidence into a deterministic,
// credential-safe promotion verdict per collector family or instance:
// implemented, partial, failed, stale, gated, disabled, permission_hidden, or
// unsupported. The catalog (DefaultCollectorCatalog, built from
// scope.AllCollectorKinds) is the spine, so every known family yields at least
// one proof and unconfigured lanes are explicit; implemented requires reducer
// readback evidence and a fixture-only lane is never promoted to implemented.
// The global status surface reports only collectors that are present, while the
// full-fleet enumeration is available to the dedicated readiness read model.
// AWSCloudScanStatus rows expose per-account, per-region, per-service AWS
// scanner liveness, throttle counts, warning state, and commit status so
// operators can separate throttling, credential failure, budget exhaustion, and
// commit failures without scanning logs.
// CollectorGenerationDeadLetterSnapshot reports commit failures that happened
// before projector work existed, so operators can separate source-level replay
// requests from normal queue replay; unresolved dead-letter and replay-request
// rows keep health degraded until a later successful source commit clears them.
// SemanticExtractionStatus reports optional LLM-assisted extraction liveness as
// unavailable when no provider is configured; when provider profiles are
// configured, it carries redacted profile state and source-policy gates without
// credential handles. That informational state never degrades health or blocks
// deterministic indexing, reducer, API, MCP, or documentation fact paths.
// AnswerNarrationStatus reports optional governed answer narration posture as
// disabled by default, with deterministic answer packets still available as the
// canonical fallback. It carries only low-cardinality state, reason, retention,
// policy hash, and validator reason-code metadata; it never carries prompts,
// provider responses, credentials, source identifiers, or canonical truth.
// GenerationLifecycleRecord, GenerationLifecycleFilter, and
// GenerationLifecyclePage define the bounded scope-generation drilldown
// contract: one ordered page of active, pending, superseded, completed, or
// failed generations joined with the owning scope identity, the per-generation
// queue rollup (GenerationQueueStatus), and the latest failure
// (GenerationLatestFailure). The filter clamps the page limit between one and
// MaxGenerationLifecycleLimit and exposes HasScopeSelector so a named scope,
// repository, or generation that matches nothing is reported as not-found
// rather than confident emptiness.
//
// ChangedSinceFilter, ChangedSinceSummary, ChangedSinceCategoryDelta, and the
// ChangedSinceClassification/ChangedSinceCategory enums define the bounded
// repository-scope changed-since contract: a diff of one prior generation's fact
// set against the current active generation's fact set, grouped into evidence
// categories (files, content entities, facts) and the closed verdict set
// (added, updated, unchanged, retired, superseded). Counts are exact;
// ChangedSinceFilter clamps the per-classification sample handles to
// MaxChangedSinceSampleLimit. The Unavailable flag distinguishes a scope with no
// current active generation from a genuinely empty delta so the surface never
// reports all-unchanged when it cannot diff. UnavailableReason is populated for
// fail-closed cases such as generation history pruned by retention.
//
// ServiceChangedSinceFilter and ServiceChangedSinceSummary define the
// service-scope variant (#1943): the same verdict set, counts, sample, and
// Unavailable shapes, but keyed by service_id and diffed over a per-service
// materialization generation lineage instead of an ingestion scope. It reports
// the ownership (ChangedSinceCategoryOwnership, #1943), deployment
// (ChangedSinceCategoryDeployment, #1985), runtime
// (ChangedSinceCategoryRuntime, #1986), dependencies
// (ChangedSinceCategoryDependencies, #1987), docs
// (ChangedSinceCategoryDocs, #1988), and incidents
// (ChangedSinceCategoryIncidents, #1989) evidence families; the remaining family
// appends to ServiceChangedSinceCategories as it lands.
//
// ControlPlane projects an already-built Report into OperatorControlPlane, the
// unified operator read model for the control-plane epic: queue depth with
// claim-latency and stuck-work signals, reducer-domain backlogs, collector-family
// promotion verdicts with the newest proof artifact, and dead-letter state classed
// by reducer domain (DomainDeadLetter) and the collector-generation commit path.
// It performs no I/O, so the operator-control-plane API route and MCP tool add no
// database or graph cost beyond the snapshot the caller already loaded.
package status
