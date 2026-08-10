// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

// SchemaVersion is the stable schema identifier for portable evidence bundles.
const SchemaVersion = "evidence_bundle.v1"

// Bundle is a share-safe, portable snapshot across bounded Eshu proof surfaces.
type Bundle struct {
	SchemaVersion string            `json:"schema_version"`
	BundleID      string            `json:"bundle_id"`
	Identity      Identity          `json:"identity"`
	Source        SourceIdentity    `json:"source"`
	Redaction     RedactionProfile  `json:"redaction"`
	Contents      Contents          `json:"contents"`
	Missing       []MissingEvidence `json:"missing_evidence"`
	Reproduce     []ReproduceCall   `json:"reproduce"`
	Bounds        Bounds            `json:"bounds"`
	Validation    Validation        `json:"validation"`
}

// Identity names the bounded bundle scope without embedding private locators.
type Identity struct {
	ScopeID   string `json:"scope_id"`
	Profile   string `json:"profile"`
	CreatedAt string `json:"created_at"`
}

// SourceIdentity records redacted source identity for reproducing calls.
type SourceIdentity struct {
	Repository string `json:"repository"`
	Deployment string `json:"deployment,omitempty"`
}

// RedactionProfile records the share-safe policy applied before serialization.
type RedactionProfile struct {
	Profile string   `json:"profile"`
	Rules   []string `json:"rules"`
}

// Contents groups the bounded surfaces packaged by the bundle.
type Contents struct {
	AnswerPackets        []PacketSummary     `json:"answer_packets"`
	InvestigationPackets []PacketSummary     `json:"investigation_packets"`
	CapabilityCatalog    CatalogSnapshot     `json:"capability_catalog"`
	SurfaceInventory     CatalogSnapshot     `json:"surface_inventory"`
	OperatorState        []OperatorStateItem `json:"operator_state"`
	// PipelineState carries deterministic queue, generation, and domain
	// backlog truth composed by BuildLiveBundle from the running stack's
	// status endpoints (#4045). It is a pointer with json:",omitempty" so a
	// demo bundle built by BuildDemoBundle never renders this field and its
	// bundle_id stays unchanged.
	PipelineState *PipelineStateSnapshot `json:"pipeline_state,omitempty"`
	// SemanticProviderState carries semantic-extraction/LLM-provider status,
	// kept as a separate field from PipelineState so operator-facing tooling
	// never conflates deterministic pipeline truth with provider posture
	// (issue #4045 requirement). Pointer with json:",omitempty" for the same
	// demo-bundle-stability reason as PipelineState.
	SemanticProviderState *SemanticProviderStateSnapshot `json:"semantic_provider_state,omitempty"`
}

// PipelineStateSnapshot is the live bundle's deterministic pipeline-truth
// section: repository count, queue depth, generation history, stage and
// domain backlogs, and collector readiness, as observed from a running
// stack's status endpoints. It deliberately excludes semantic/provider
// status (see SemanticProviderStateSnapshot) and per-kind fact counts, which
// no status endpoint exposes (recorded instead as a "fact_counts"
// MissingEvidence entry).
type PipelineStateSnapshot struct {
	RepositoryCount   int                                  `json:"repository_count"`
	HealthState       string                               `json:"health_state"`
	HealthReasons     []string                             `json:"health_reasons,omitempty"`
	Queue             PipelineQueueSnapshot                `json:"queue"`
	QueueBlockedCount int                                  `json:"queue_blocked_count,omitempty"`
	ScopeActivity     PipelineScopeActivitySnapshot        `json:"scope_activity,omitempty"`
	GenerationHistory PipelineGenerationHistorySnapshot    `json:"generation_history,omitempty"`
	StageSummaries    []PipelineStageSummarySnapshot       `json:"stage_summaries,omitempty"`
	DomainBacklogs    []PipelineDomainBacklogSnapshot      `json:"domain_backlogs,omitempty"`
	Collectors        []PipelineCollectorReadinessSnapshot `json:"collectors,omitempty"`
}

// PipelineQueueSnapshot records reducer/ingest queue depth by state.
type PipelineQueueSnapshot struct {
	Pending    int `json:"pending"`
	InFlight   int `json:"in_flight"`
	Retrying   int `json:"retrying"`
	Succeeded  int `json:"succeeded"`
	Failed     int `json:"failed"`
	DeadLetter int `json:"dead_letter"`
}

// PipelineScopeActivitySnapshot records repo-scope activity counts.
type PipelineScopeActivitySnapshot struct {
	Active    int `json:"active"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

// PipelineGenerationHistorySnapshot records generation lifecycle counts.
type PipelineGenerationHistorySnapshot struct {
	Active     int `json:"active"`
	Pending    int `json:"pending"`
	Completed  int `json:"completed"`
	Superseded int `json:"superseded"`
	Failed     int `json:"failed"`
	Other      int `json:"other"`
}

// PipelineStageSummarySnapshot records one reducer stage's backlog.
type PipelineStageSummarySnapshot struct {
	Stage      string `json:"stage"`
	Pending    int    `json:"pending"`
	Claimed    int    `json:"claimed"`
	Running    int    `json:"running"`
	Retrying   int    `json:"retrying"`
	Succeeded  int    `json:"succeeded"`
	Failed     int    `json:"failed"`
	DeadLetter int    `json:"dead_letter"`
}

// PipelineDomainBacklogSnapshot records one materialization domain's backlog.
type PipelineDomainBacklogSnapshot struct {
	Domain      string `json:"domain"`
	Outstanding int    `json:"outstanding"`
	InFlight    int    `json:"in_flight"`
	Retrying    int    `json:"retrying"`
	Failed      int    `json:"failed"`
	DeadLetter  int    `json:"dead_letter"`
}

// PipelineCollectorReadinessSnapshot records one collector's readiness
// classification without embedding any host, instance address, or endpoint.
type PipelineCollectorReadinessSnapshot struct {
	CollectorKind  string `json:"collector_kind"`
	StatusCategory string `json:"status_category"`
	Health         string `json:"health"`
}

// SemanticProviderStateSnapshot is the live bundle's semantic-extraction and
// LLM-provider posture section, kept separate from PipelineState (see
// Contents.SemanticProviderState doc). "unavailable" with reason
// "provider_not_configured" is the no-provider-configured state, distinct
// from a configured-but-unhealthy provider.
type SemanticProviderStateSnapshot struct {
	State              string                            `json:"state"`
	Reason             string                            `json:"reason,omitempty"`
	ProviderConfigured bool                              `json:"provider_configured"`
	ProviderProfiles   []SemanticProviderProfileSnapshot `json:"provider_profiles,omitempty"`
}

// SemanticProviderProfileSnapshot records one configured provider profile's
// state without embedding credentials or endpoint locators.
type SemanticProviderProfileSnapshot struct {
	ProfileID    string `json:"profile_id"`
	ProviderKind string `json:"provider_kind"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
}

// PacketSummary is a bounded, redacted packet identity and handle summary.
type PacketSummary struct {
	Family          string   `json:"family"`
	Schema          string   `json:"schema"`
	TruthClass      string   `json:"truth_class"`
	Summary         string   `json:"summary"`
	EvidenceHandles []string `json:"evidence_handles"`
	NextCalls       []string `json:"next_calls"`
}

// CatalogSnapshot carries a compact catalog or inventory fingerprint.
type CatalogSnapshot struct {
	Schema       string   `json:"schema"`
	EntryCount   int      `json:"entry_count"`
	SurfaceCount int      `json:"surface_count,omitempty"`
	Handles      []string `json:"handles"`
}

// OperatorStateItem records freshness, readiness, or limitation state.
type OperatorStateItem struct {
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// MissingEvidence records a named gap without hiding it behind a summary.
type MissingEvidence struct {
	Family string `json:"family"`
	Reason string `json:"reason"`
}

// ReproduceCall names a bounded command, route, or MCP tool.
type ReproduceCall struct {
	Kind   string            `json:"kind"`
	Target string            `json:"target"`
	Args   map[string]string `json:"args,omitempty"`
}

// Bounds records per-layer caps and truncation state.
type Bounds struct {
	MaxAnswerPackets        int      `json:"max_answer_packets"`
	MaxInvestigationPackets int      `json:"max_investigation_packets"`
	MaxHandles              int      `json:"max_handles"`
	Truncated               bool     `json:"truncated"`
	TruncatedLayers         []string `json:"truncated_layers,omitempty"`
}

// Validation records deterministic bundle validation checks.
type Validation struct {
	Status string   `json:"status"`
	Checks []string `json:"checks"`
}

// DemoBundleOptions controls deterministic demo bundle construction.
type DemoBundleOptions struct {
	ScopeID string
}
