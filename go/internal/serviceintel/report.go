// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import "github.com/eshu-hq/eshu/go/internal/query"

// ReportSchema is the wire schema identifier for a service intelligence report.
// It is versioned so consumers can pin against contract changes.
const ReportSchema = "service_intelligence_report.v1"

// SectionKind identifies one fixed section of a service intelligence report.
// The set is closed and ordered: the composer always emits every kind in the
// canonical order so the same source evidence yields a byte-identical report.
type SectionKind string

const (
	// SectionIdentity anchors the report on the canonical service identity from
	// the service story dossier. It is the report's truth anchor.
	SectionIdentity SectionKind = "identity"
	// SectionCodeToRuntime carries the code-to-runtime trace: entrypoints,
	// network paths, and the evidence that links source to running surface.
	SectionCodeToRuntime SectionKind = "code_to_runtime"
	// SectionDeploymentConfig carries deployment lanes and configuration
	// influence: environments, platforms, and the deployment evidence behind them.
	SectionDeploymentConfig SectionKind = "deployment_config"
	// SectionSupplyChain carries supply-chain evidence: image, dependency, and
	// build-provenance findings tied to the service.
	SectionSupplyChain SectionKind = "supply_chain"
	// SectionIncidentsSupport carries incident, support, and runbook evidence
	// routed to the service.
	SectionIncidentsSupport SectionKind = "incidents_support"
)

// SectionStatus is the composed status of a report section. It is derived from
// the embedded AnswerPacket, never reclassified from the source truth.
type SectionStatus string

const (
	// StatusSupported marks a section backed by resolved, fresh evidence.
	StatusSupported SectionStatus = "supported"
	// StatusPartial marks a usable but incomplete section: truncated, stale, or
	// resolved with no supporting evidence.
	StatusPartial SectionStatus = "partial"
	// StatusUnsupported marks a section whose source route errored or produced no
	// truth to classify.
	StatusUnsupported SectionStatus = "unsupported"
)

// ReportSubject identifies the service the report is about. The composer copies
// it verbatim from the caller; it does not resolve or invent identity.
type ReportSubject struct {
	// ServiceName is the canonical service name. Required.
	ServiceName string `json:"service_name"`
	// ServiceID is the canonical service identifier, when known.
	ServiceID string `json:"service_id,omitempty"`
	// RepoID is the owning repository identifier, when known.
	RepoID string `json:"repo_id,omitempty"`
	// RepoName is the owning repository name, when known.
	RepoName string `json:"repo_name,omitempty"`
}

// NextCall is a bounded, executable follow-up call. It mirrors the
// evidence-citation recommended_next_calls shape and adds an optional playbook
// handle plus bounded input arguments so a suggestion can point at a
// deterministic query playbook. The composer never emits a NextCall that cannot
// be executed against a real tool, route, or playbook.
type NextCall struct {
	// Tool is the MCP tool to call, when the next step is an MCP tool.
	Tool string `json:"tool,omitempty"`
	// Route is the HTTP route to call, when the next step is an API route.
	Route string `json:"route,omitempty"`
	// Playbook is the deterministic query playbook id, when one applies.
	Playbook string `json:"playbook,omitempty"`
	// Reason explains why this call is recommended.
	Reason string `json:"reason,omitempty"`
	// Arguments carries bounded, non-sensitive input values needed to execute
	// the call.
	Arguments map[string]any `json:"arguments,omitempty"`
}

// SectionInput carries the evidence for one section, already produced by an
// existing answer route (service story, citation packet, supply-chain inventory,
// incident context). The composer arranges these inputs into a report; it does
// not query, re-derive, or reclassify them.
type SectionInput struct {
	// Kind selects which fixed section this input fills. An unknown kind is
	// ignored by the composer.
	Kind SectionKind
	// Summary is the proposed human sentence for the section. The composer drops
	// it whenever the section is unsupported or partial-with-no-evidence so an
	// unanswerable section never reads as confident prose.
	Summary string
	// Truth is the source route's TruthEnvelope. It is preserved on the section,
	// never reclassified. A nil Truth (with no Err) marks the section empty.
	Truth *query.TruthEnvelope
	// Evidence are the resolved evidence handles backing the section.
	Evidence []query.EvidenceCitationHandle
	// MissingEvidence lists evidence handles requested but not resolved.
	MissingEvidence []query.EvidenceCitationHandle
	// Truncated marks the source result as truncated.
	Truncated bool
	// NoEvidence marks an evidence-centric section that resolved nothing. The
	// composer then marks the section partial and drops the confident summary.
	NoEvidence bool
	// Limitations carries bounded human-readable caveats from the source route.
	Limitations []string
	// NextCalls are caller-supplied bounded follow-up calls for the section.
	NextCalls []NextCall
	// HighImpact marks a section that carries a high-confidence, high-impact
	// relationship worth a guided drilldown. The composer does not judge impact
	// itself; the caller, which holds the edge data, sets this flag so a
	// high_impact_relationship investigation is grounded in real evidence.
	HighImpact bool
	// Err, when set, marks the section unavailable because the source route
	// errored. It takes precedence over Truth.
	Err *query.ErrorEnvelope
}

// ReportInput is the full composition input: the subject plus the per-section
// evidence gathered from existing answer routes.
type ReportInput struct {
	// Subject identifies the service. Required; ServiceName must be set.
	Subject ReportSubject
	// Sections carries the per-section evidence. Missing sections are emitted as
	// unsupported with an explicit limitation and a recommended next call.
	Sections []SectionInput
}

// ReportSection is one composed section. It embeds a canonical AnswerPacket so
// the section inherits the proven honesty contract (no confident summary on an
// unsupported or empty section) and preserves truth, evidence handles,
// limitations, and recommended next calls without duplicating that logic.
type ReportSection struct {
	// Kind is the fixed section kind.
	Kind SectionKind `json:"kind"`
	// Title is the human-readable section title.
	Title string `json:"title"`
	// Status is the composed status derived from the embedded AnswerPacket.
	Status SectionStatus `json:"status"`
	// Answer is the canonical answer packet for the section. It is the source of
	// truth for the section's truth class, evidence, limitations, and next calls.
	Answer query.AnswerPacket `json:"answer"`
}

// Report is an operator-ready service intelligence artifact composed from
// existing answer evidence. It never introduces a new truth source: each
// section's truth is the source route's truth, and the report-level truth is
// anchored on the identity section (the service story). Unsupported and empty
// sections stay visible with explicit limitations and bounded next calls rather
// than being rewritten as confident prose.
type Report struct {
	// Schema is the wire schema identifier (ReportSchema).
	Schema string `json:"schema"`
	// Subject identifies the service the report describes.
	Subject ReportSubject `json:"subject"`
	// Supported is true when the identity anchor resolved a service. A report for
	// an unknown service is unsupported.
	Supported bool `json:"supported"`
	// Partial is true when any present section is partial or unsupported, so the
	// report never reads as complete while sections are missing.
	Partial bool `json:"partial"`
	// TruthClass is the identity section's truth class, copied not reclassified.
	TruthClass query.AnswerTruthClass `json:"truth_class"`
	// Truth is a copy of the identity section's TruthEnvelope, or nil when the
	// identity anchor is unsupported.
	Truth *query.TruthEnvelope `json:"truth,omitempty"`
	// Sections carries every fixed section in canonical order.
	Sections []ReportSection `json:"sections"`
	// Limitations aggregates the de-duplicated bounded caveats across sections so
	// an operator can scan what the report cannot prove.
	Limitations []string `json:"limitations,omitempty"`
	// NextCalls aggregates the de-duplicated bounded follow-up calls across
	// sections, each traceable to a real tool, route, or playbook.
	NextCalls []NextCall `json:"recommended_next_calls,omitempty"`
	// Investigations are the deterministic, de-duplicated, bounded guided
	// investigations derived from the report's evidence gaps, stale freshness,
	// ambiguous targets, unsupported lanes, and flagged high-impact
	// relationships. It is empty when no section carries a supporting basis.
	Investigations []SuggestedInvestigation `json:"suggested_investigations,omitempty"`
}
