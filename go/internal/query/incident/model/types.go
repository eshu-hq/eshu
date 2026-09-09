// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Capability is the query capability gating incident-context reads. Its home
// is incident/model/ (#6060, lane B S2); the query root keeps the
// lowercase spelling for its staying contract matrix and handler callers.
const Capability = "incident.context.read"

// DefaultLimit bounds an incident-context read when the caller passes no
// limit. Its home is incident/model/; see Capability.
const DefaultLimit = 25

// MaxLimit is the largest incident-context limit a caller may request. Its
// home is incident/model/; see Capability.
const MaxLimit = 100

// ProviderPagerDuty is the default incident provider. It lives in
// incident/model/ (#6060, lane B S2) because both NormalizeFilter below and
// the incident/store/ row decoders fall back to it, and the two must never
// disagree on the default.
const ProviderPagerDuty = "pagerduty"

// IncidentContextStore reads bounded incident context evidence.
type IncidentContextStore interface {
	ReadIncidentContext(context.Context, IncidentContextFilter) (IncidentContextSnapshot, error)
}

// IncidentRepositoryAuthorizer resolves the durable owning repositories an
// incident correlates to through the reducer-owned incident→repository
// correlation edge (reducer_incident_repository_correlation). Only confident
// exact/derived edges are returned; ambiguous, unresolved, rejected, or
// name-fingerprint-only routing carries no durable repository and yields an empty
// result so a scoped-token read fails closed (not-found) instead of leaking an
// incident bound only to a fuzzy service-name match. It is the query-side seam
// over the durable edge landed by the incident-repository correlation reducer.
type IncidentRepositoryAuthorizer interface {
	// ResolveDurableIncidentRepositories returns the distinct durable repository
	// ids correlated to one incident. provider and providerIncidentID identify the
	// incident; scopeID optionally narrows the incident anchor when the same
	// provider incident id appears in more than one ingestion scope. An empty,
	// non-error result means the incident has no durable owning repository.
	ResolveDurableIncidentRepositories(
		ctx context.Context,
		provider string,
		providerIncidentID string,
		scopeID string,
	) ([]string, error)
}

// IncidentContextFilter bounds one incident-context read.
type IncidentContextFilter struct {
	Provider           string
	ProviderIncidentID string
	ScopeID            string
	ServiceID          string
	Since              string
	Until              string
	Limit              int
}

// IncidentContextQuery is the normalized query echoed in the response.
type IncidentContextQuery struct {
	Provider           string `json:"provider"`
	ProviderIncidentID string `json:"provider_incident_id"`
	ScopeID            string `json:"scope_id,omitempty"`
	ServiceID          string `json:"service_id,omitempty"`
	Since              string `json:"since,omitempty"`
	Until              string `json:"until,omitempty"`
	Limit              int    `json:"limit"`
}

// IncidentTruthLabel classifies one incident-context evidence edge.
type IncidentTruthLabel string

const (
	IncidentTruthExact            IncidentTruthLabel = "exact"
	IncidentTruthDerived          IncidentTruthLabel = "derived"
	IncidentTruthFallback         IncidentTruthLabel = "fallback"
	IncidentTruthDrifted          IncidentTruthLabel = "drifted"
	IncidentTruthAmbiguous        IncidentTruthLabel = "ambiguous"
	IncidentTruthUnresolved       IncidentTruthLabel = "unresolved"
	IncidentTruthStale            IncidentTruthLabel = "stale"
	IncidentTruthRejected         IncidentTruthLabel = "rejected"
	IncidentTruthPermissionHidden IncidentTruthLabel = "permission_hidden"
	IncidentTruthMissing          IncidentTruthLabel = "missing"
)

// IncidentEvidenceSlot names one position in the incident evidence path.
type IncidentEvidenceSlot string

const (
	IncidentSlotIncident        IncidentEvidenceSlot = "incident"
	IncidentSlotService         IncidentEvidenceSlot = "service"
	IncidentSlotIntendedRouting IncidentEvidenceSlot = "intended_routing"
	IncidentSlotAppliedRouting  IncidentEvidenceSlot = "applied_routing"
	IncidentSlotLiveRouting     IncidentEvidenceSlot = "live_routing"
	IncidentSlotDeployable      IncidentEvidenceSlot = "deployable"
	IncidentSlotRuntimeArtifact IncidentEvidenceSlot = "runtime_artifact"
	IncidentSlotImage           IncidentEvidenceSlot = "image"
	IncidentSlotBuildDeploy     IncidentEvidenceSlot = "build_deploy"
	IncidentSlotCommit          IncidentEvidenceSlot = "commit"
	IncidentSlotPullRequest     IncidentEvidenceSlot = "pull_request"
	IncidentSlotWorkItem        IncidentEvidenceSlot = "work_item"
)

// IncidentContextSnapshot is the store-owned evidence packet before response
// defaults and missing slots are applied.
type IncidentContextSnapshot struct {
	Query          IncidentContextQuery             `json:"query"`
	Incident       IncidentContextIncident          `json:"incident"`
	Timeline       []IncidentContextTimelineEvent   `json:"timeline,omitempty"`
	RelatedChanges []IncidentContextChangeCandidate `json:"related_changes,omitempty"`
	EvidencePath   []IncidentContextEvidenceEdge    `json:"evidence_path,omitempty"`
	Truncated      bool                             `json:"truncated,omitempty"`
}

// IncidentContextResponse is the public API/MCP response for one incident.
type IncidentContextResponse struct {
	Query             IncidentContextQuery             `json:"query"`
	Incident          IncidentContextIncident          `json:"incident"`
	Timeline          []IncidentContextTimelineEvent   `json:"timeline"`
	RelatedChanges    []IncidentContextChangeCandidate `json:"related_changes"`
	EvidencePath      []IncidentContextEvidenceEdge    `json:"evidence_path"`
	MissingEvidence   []IncidentMissingEvidence        `json:"missing_evidence"`
	AmbiguousEvidence []IncidentContextEvidenceEdge    `json:"ambiguous_evidence"`
	Truncated         bool                             `json:"truncated"`
	AnswerMetadata    querycontract.AnswerMetadata     `json:"answer_metadata"`
}

// IncidentContextIncident is the provider-reported incident anchor.
type IncidentContextIncident struct {
	Provider           string                     `json:"provider"`
	ProviderIncidentID string                     `json:"provider_incident_id"`
	ScopeID            string                     `json:"scope_id,omitempty"`
	IncidentNumber     int64                      `json:"incident_number,omitempty"`
	Title              string                     `json:"title,omitempty"`
	Status             string                     `json:"status,omitempty"`
	Urgency            string                     `json:"urgency,omitempty"`
	Priority           IncidentContextReference   `json:"priority,omitempty"`
	Service            IncidentContextReference   `json:"service,omitempty"`
	EscalationPolicy   IncidentContextReference   `json:"escalation_policy,omitempty"`
	Teams              []IncidentContextReference `json:"teams,omitempty"`
	Assignments        []IncidentContextReference `json:"assignments,omitempty"`
	CreatedAt          string                     `json:"created_at,omitempty"`
	UpdatedAt          string                     `json:"updated_at,omitempty"`
	ResolvedAt         string                     `json:"resolved_at,omitempty"`
	SourceURL          string                     `json:"source_url,omitempty"`
	EvidenceFactID     string                     `json:"evidence_fact_id,omitempty"`
	SourceConfidence   string                     `json:"source_confidence,omitempty"`
	ObservedAt         string                     `json:"observed_at,omitempty"`
}

// IncidentContextReference is a bounded provider reference.
type IncidentContextReference struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Summary string `json:"summary,omitempty"`
	URL     string `json:"url,omitempty"`
}

// IncidentContextTimelineEvent is one provider-reported incident event.
type IncidentContextTimelineEvent struct {
	EventID          string                   `json:"event_id"`
	EventType        string                   `json:"event_type,omitempty"`
	Actor            IncidentContextReference `json:"actor,omitempty"`
	Channel          string                   `json:"channel,omitempty"`
	Summary          string                   `json:"summary,omitempty"`
	CreatedAt        string                   `json:"created_at,omitempty"`
	SourceURL        string                   `json:"source_url,omitempty"`
	EvidenceFactID   string                   `json:"evidence_fact_id,omitempty"`
	SourceConfidence string                   `json:"source_confidence,omitempty"`
	ObservedAt       string                   `json:"observed_at,omitempty"`
}

// IncidentContextChangeCandidate is a related or time-window candidate change.
type IncidentContextChangeCandidate struct {
	ChangeID         string                     `json:"change_id"`
	Summary          string                     `json:"summary,omitempty"`
	Source           string                     `json:"source,omitempty"`
	Services         []IncidentContextReference `json:"services,omitempty"`
	Links            []IncidentContextLink      `json:"links,omitempty"`
	Timestamp        string                     `json:"timestamp,omitempty"`
	SourceURL        string                     `json:"source_url,omitempty"`
	TruthLabel       IncidentTruthLabel         `json:"truth_label"`
	Explanation      string                     `json:"explanation"`
	EvidenceFactID   string                     `json:"evidence_fact_id,omitempty"`
	SourceConfidence string                     `json:"source_confidence,omitempty"`
	ObservedAt       string                     `json:"observed_at,omitempty"`
}

// IncidentContextLink is a sanitized provider link.
type IncidentContextLink struct {
	Href string `json:"href,omitempty"`
	Text string `json:"text,omitempty"`
}

// IncidentContextEvidenceEdge explains one slot in the evidence path.
type IncidentContextEvidenceEdge struct {
	Slot        IncidentEvidenceSlot               `json:"slot"`
	TruthLabel  IncidentTruthLabel                 `json:"truth_label"`
	Explanation string                             `json:"explanation"`
	Value       map[string]string                  `json:"value,omitempty"`
	Evidence    []IncidentContextEvidenceRef       `json:"evidence,omitempty"`
	Candidates  []IncidentContextEvidenceCandidate `json:"candidates,omitempty"`
}

// IncidentContextEvidenceRef points to one source or reducer fact behind an edge.
type IncidentContextEvidenceRef struct {
	FactID     string `json:"fact_id,omitempty"`
	RecordID   string `json:"record_id,omitempty"`
	Source     string `json:"source,omitempty"`
	Kind       string `json:"kind,omitempty"`
	URL        string `json:"url,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
}

// IncidentContextEvidenceCandidate describes an ambiguous evidence candidate.
type IncidentContextEvidenceCandidate struct {
	ID     string `json:"id,omitempty"`
	Label  string `json:"label,omitempty"`
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// IncidentMissingEvidence names evidence that was not present for the incident.
type IncidentMissingEvidence struct {
	Slot   IncidentEvidenceSlot `json:"slot"`
	Reason string               `json:"reason"`
}

// IncidentContextIncidentCandidate identifies an ambiguous incident anchor.
type IncidentContextIncidentCandidate struct {
	Provider           string `json:"provider"`
	ProviderIncidentID string `json:"provider_incident_id"`
	ScopeID            string `json:"scope_id,omitempty"`
	ServiceID          string `json:"service_id,omitempty"`
	ServiceName        string `json:"service_name,omitempty"`
	SourceURL          string `json:"source_url,omitempty"`
	EvidenceFactID     string `json:"evidence_fact_id,omitempty"`
}

// ErrIncidentContextNotFound reports a missing incident anchor. It lives in
// incident/model/ (#6060, lane B S2) with the store contract: the
// incident/store/ reads return it and the incident/ handler matches it, so
// neither leaf may own it alone.
var ErrIncidentContextNotFound = errors.New("incident context not found")

// IncidentContextAmbiguousError reports multiple active incident anchors.
// It lives in incident/model/ with the store contract; see
// ErrIncidentContextNotFound.
type IncidentContextAmbiguousError struct {
	ProviderIncidentID string
	Candidates         []IncidentContextIncidentCandidate
}

func (e IncidentContextAmbiguousError) Error() string {
	return fmt.Sprintf("incident %q matched multiple active provider scopes; pass scope_id", e.ProviderIncidentID)
}

// NormalizeFilter trims and defaults one incident-context filter. It lives
// in incident/model/ (#6060, lane B S2) because both the incident/ handler
// (which authorizes the normalized provider, incident id, and scope before
// any read) and the incident/store/ reads apply it, and the two must never
// disagree on what a filter means.
func NormalizeFilter(filter IncidentContextFilter) IncidentContextFilter {
	filter.Provider = strings.ToLower(strings.TrimSpace(filter.Provider))
	if filter.Provider == "" {
		filter.Provider = ProviderPagerDuty
	}
	filter.ProviderIncidentID = strings.TrimSpace(filter.ProviderIncidentID)
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.ServiceID = strings.TrimSpace(filter.ServiceID)
	filter.Since = strings.TrimSpace(filter.Since)
	filter.Until = strings.TrimSpace(filter.Until)
	return filter
}
