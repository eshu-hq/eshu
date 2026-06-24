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
