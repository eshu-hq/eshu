// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cloudRuntimeDriftReadbackCapability is the conformance-matrix capability id for
// the provider-neutral multi-cloud runtime drift readback (issues #1997, #1998).
// It is gated to reducer-owning profiles; local_lightweight returns
// unsupported_capability because it cannot materialize the
// reducer_multi_cloud_runtime_drift_finding rows the readback lists.
const cloudRuntimeDriftReadbackCapability = "cloud_runtime_drift.readback.list"

const (
	cloudRuntimeDriftDefaultLimit = 100
	cloudRuntimeDriftMaxLimit     = 500
)

// cloudRuntimeDriftProviders is the closed set of providers the runtime drift
// readback accepts as a filter. It matches the multi-cloud collector contract
// providers; an unrecognized provider is rejected as invalid input rather than
// silently ignored so a typo can never widen the scan across providers.
var cloudRuntimeDriftProviders = map[string]struct{}{
	"aws":   {},
	"gcp":   {},
	"azure": {},
}

// MultiCloudRuntimeDriftFilter bounds a provider-neutral runtime drift readback.
// ScopeID is required; the store and handler both reject an empty scope so an
// unbounded provider scan can never reach the read surface.
type MultiCloudRuntimeDriftFilter struct {
	// ScopeID is the required canonical ingestion scope. account_id, project_id,
	// and subscription_id are accepted request aliases that target this field.
	ScopeID string
	// Provider optionally restricts the read to one of aws, gcp, or azure.
	Provider string
	// CloudResourceUID optionally pins the read to one canonical resource.
	CloudResourceUID string
	// FindingKinds optionally restricts the read to a closed set of finding kinds.
	FindingKinds []string
	// Limit caps the page size; the handler defaults and bounds it.
	Limit int
	// Offset is the keyset continuation offset.
	Offset int
}

// MultiCloudRuntimeDriftFindingRow is one query-facing provider-neutral runtime
// drift finding loaded from the reducer-owned drift fact. RawIdentity and the raw
// evidence atoms are intentionally absent: the readback projects only canonical,
// non-sensitive fields and never echoes raw provider locators.
type MultiCloudRuntimeDriftFindingRow struct {
	// FactID is the durable reducer fact id for the finding.
	FactID string
	// ScopeID is the canonical ingestion scope the finding belongs to.
	ScopeID string
	// GenerationID is the active generation the finding was materialized in.
	GenerationID string
	// SourceSystem names the collector source that produced the evidence.
	SourceSystem string
	// Provider is the cloud provider (aws, gcp, or azure).
	Provider string
	// CloudResourceUID is the canonical, provider-neutral resource identity. It is
	// already normalized by the reducer and is safe to surface.
	CloudResourceUID string
	// RawIdentity is the raw provider locator. It is loaded for completeness but
	// MUST NOT be projected onto the wire; the view drops it.
	RawIdentity string
	// FindingKind is one of orphaned/unmanaged/ambiguous/unknown_cloud_resource.
	FindingKind string
	// ManagementStatus is the provider-neutral management status the reducer
	// recorded (for example terraform_state_only or ambiguous_management).
	ManagementStatus string
	// Confidence is the reducer confidence for the finding.
	Confidence float64
	// MatchedTerraformStateAddress is the matched state resource address, if any.
	MatchedTerraformStateAddress string
	// MissingEvidence lists the evidence families absent for this finding.
	MissingEvidence []string
	// WarningFlags are reducer-emitted safety warnings (for example
	// ambiguous_ownership) that feed the refusal posture.
	WarningFlags []string
	// RecommendedAction is the read-only triage hint the reducer attached.
	RecommendedAction string
	// DriftedAttributes carries the bounded declared/observed value pairs for
	// an image_version_drift finding (for example ami, image_uri, version,
	// or the ECS container image comparison). Empty for every other finding
	// kind. This is a narrow, purpose-built projection of the reducer's
	// declared_/observed_ evidence atoms -- NOT a general evidence-atom
	// readback (#5453); see CloudRuntimeDriftFindingView for the wire
	// contract this feeds.
	DriftedAttributes []DriftedAttributeView
}

// DriftedAttributeView is one declared/observed value pair for an
// image_version_drift finding's comparable attribute (ami, image_uri,
// version, or the synthetic "image" key for the ECS container-image
// comparison).
type DriftedAttributeView struct {
	// Attribute is the allowlisted comparable attribute name.
	Attribute string `json:"attribute"`
	// Declared is the Terraform-state value.
	Declared string `json:"declared_value"`
	// Observed is the AWS-observed cloud value.
	Observed string `json:"observed_value"`
}

// MultiCloudRuntimeDriftStore reads active reducer-materialized provider-neutral
// runtime drift findings. The query handler depends on this narrow interface so
// unit tests can supply a fixture-backed reader without a live database, and the
// Postgres adapter implements it over reducer_multi_cloud_runtime_drift_finding
// facts.
type MultiCloudRuntimeDriftStore interface {
	// ListActiveMultiCloudRuntimeDriftFindings returns one bounded page of active
	// findings for the caller's scope.
	ListActiveMultiCloudRuntimeDriftFindings(
		ctx context.Context,
		filter MultiCloudRuntimeDriftFilter,
	) ([]MultiCloudRuntimeDriftFindingRow, error)
	// CountActiveMultiCloudRuntimeDriftFindings returns the total active finding
	// count for the same bounded filters, used to report truncation.
	CountActiveMultiCloudRuntimeDriftFindings(
		ctx context.Context,
		filter MultiCloudRuntimeDriftFilter,
	) (int, error)
}

// CloudRuntimeDriftHandler serves a bounded, paginated, truth-labeled readback of
// provider-neutral runtime drift findings from the reducer-owned
// reducer_multi_cloud_runtime_drift_finding rows. It is read-only and never
// fabricates truth: it projects only reducer-resolved canonical fields, never
// raw provider locators, and refuses unsafe findings (reporting them as rejected
// with a refused action) rather than silently omitting them.
type CloudRuntimeDriftHandler struct {
	// Store reads active provider-neutral runtime drift findings.
	Store MultiCloudRuntimeDriftStore
	// Profile selects the active runtime profile for capability gating.
	Profile QueryProfile
}

// cloudRuntimeDriftRequest is the bounded request body for the readback.
type cloudRuntimeDriftRequest struct {
	ScopeID          string   `json:"scope_id"`
	AccountID        string   `json:"account_id"`
	ProjectID        string   `json:"project_id"`
	SubscriptionID   string   `json:"subscription_id"`
	Provider         string   `json:"provider"`
	CloudResourceUID string   `json:"cloud_resource_uid"`
	FindingKinds     []string `json:"finding_kinds"`
	Limit            int      `json:"limit"`
	Offset           int      `json:"offset"`
}

// CloudRuntimeDriftFindingView is the bounded, non-sensitive wire shape for one
// provider-neutral runtime drift finding. It carries the canonical identity, the
// finding classification, the provider-neutral source state, and the
// safety/refusal posture, but never the raw provider locator or raw evidence
// atoms.
type CloudRuntimeDriftFindingView struct {
	FactID                       string   `json:"fact_id"`
	Provider                     string   `json:"provider"`
	ScopeID                      string   `json:"scope_id"`
	GenerationID                 string   `json:"generation_id"`
	SourceSystem                 string   `json:"source_system,omitempty"`
	CloudResourceUID             string   `json:"cloud_resource_uid"`
	FindingKind                  string   `json:"finding_kind"`
	ManagementStatus             string   `json:"management_status"`
	Confidence                   float64  `json:"confidence"`
	SourceState                  string   `json:"source_state"`
	MatchedTerraformStateAddress string   `json:"matched_terraform_state_address,omitempty"`
	MissingEvidence              []string `json:"missing_evidence,omitempty"`
	RecommendedAction            string   `json:"recommended_action,omitempty"`
	// DriftedAttributes carries the bounded declared/observed value pairs for
	// an image_version_drift finding (#5453). Empty for orphaned/unmanaged/
	// unknown/ambiguous findings, which carry no comparable value evidence.
	DriftedAttributes []DriftedAttributeView  `json:"drifted_attributes,omitempty"`
	SafetyGate        IaCManagementSafetyGate `json:"safety_gate"`
}

// Mount registers the provider-neutral runtime drift readback route.
func (h *CloudRuntimeDriftHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/cloud/runtime-drift/findings", h.listFindings)
	mux.HandleFunc("GET /api/v0/investigations/drift/packet", h.getDriftPacket)
}

func (h *CloudRuntimeDriftHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

// listFindings serves the bounded, filterable, paginated readback of
// provider-neutral runtime drift findings.
//
// POST /api/v0/cloud/runtime-drift/findings
func (h *CloudRuntimeDriftHandler) listFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCloudRuntimeDriftFindings,
		"POST /api/v0/cloud/runtime-drift/findings",
		cloudRuntimeDriftReadbackCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cloudRuntimeDriftReadbackCapability) {
		h.writeContractError(
			w, r,
			http.StatusNotImplemented,
			"cloud runtime drift readback requires reducer-materialized provider-neutral drift facts",
			ErrorCodeUnsupportedCapability,
		)
		return
	}

	var req cloudRuntimeDriftRequest
	if err := ReadJSON(r, &req); err != nil {
		h.writeContractError(w, r, http.StatusBadRequest, err.Error(), ErrorCodeInvalidArgument)
		return
	}
	filter, err := normalizeCloudRuntimeDriftRequest(req)
	if err != nil {
		h.writeContractError(w, r, http.StatusBadRequest, err.Error(), ErrorCodeInvalidArgument)
		return
	}
	if h == nil || h.Store == nil {
		h.writeContractError(
			w, r,
			http.StatusNotImplemented,
			"cloud runtime drift readback requires the reducer drift finding read model",
			ErrorCodeReadModelUnavailable,
		)
		return
	}

	// Access scoping (#5167 Group B): MultiCloudRuntimeDriftStore is shared
	// with GET /api/v0/investigations/drift/packet (investigation_packet_api_drift.go),
	// which this workstream does not own, so the fix is a caller-side grant
	// precheck against the caller-supplied filter.ScopeID rather than a
	// store/filter-level change. filter.ScopeID is always non-empty here
	// (normalizeCloudRuntimeDriftRequest requires it). A scoped caller with no
	// grants, or whose requested scope is outside its granted repositories/
	// ingestion scopes, gets the same zero-finding page a real empty result
	// would produce -- no existence disclosure, no store read.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() || (access.scoped() && !access.allowsRepositoryID(filter.ScopeID)) {
		h.writeCloudRuntimeDriftFindings(w, r, filter, nil, 0)
		return
	}

	total, err := h.Store.CountActiveMultiCloudRuntimeDriftFindings(r.Context(), filter)
	if err != nil {
		h.writeContractError(w, r, http.StatusInternalServerError, "cloud runtime drift readback failed", ErrorCodeInternalError)
		return
	}
	rows, err := h.Store.ListActiveMultiCloudRuntimeDriftFindings(r.Context(), filter)
	if err != nil {
		h.writeContractError(w, r, http.StatusInternalServerError, "cloud runtime drift readback failed", ErrorCodeInternalError)
		return
	}
	h.writeCloudRuntimeDriftFindings(w, r, filter, rows, total)
}

// writeCloudRuntimeDriftFindings renders the bounded findings-page response.
// Passing nil rows and total 0 renders the same zero-finding shape a real
// empty result would, used by both the genuine-empty-result path and the
// #5167 access-scoping precheck in listFindings.
func (h *CloudRuntimeDriftHandler) writeCloudRuntimeDriftFindings(
	w http.ResponseWriter,
	r *http.Request,
	filter MultiCloudRuntimeDriftFilter,
	rows []MultiCloudRuntimeDriftFindingRow,
	total int,
) {
	views := cloudRuntimeDriftFindingViews(rows)

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id":             filter.ScopeID,
		"provider":             filter.Provider,
		"cloud_resource_uid":   filter.CloudResourceUID,
		"finding_kinds":        filter.FindingKinds,
		"story":                cloudRuntimeDriftStory(filter, views, total),
		"source_state_groups":  cloudRuntimeDriftSourceStateGroups(views),
		"drift_findings":       views,
		"findings_count":       len(views),
		"total_findings_count": total,
		"limit":                filter.Limit,
		"offset":               filter.Offset,
		"truncated":            cloudRuntimeDriftTruncated(filter.Offset, len(views), total),
		"next_offset":          cloudRuntimeDriftNextOffset(filter.Offset, len(views), total),
		"truth_basis":          "materialized_reducer_rows",
		"analysis_status":      "materialized_multi_cloud_runtime_drift",
		"limitations": []string{
			"bounded to active provider-neutral runtime drift reducer facts for the requested scope",
			"source_state is derived from management status and the safety gate without promoting ownership",
			"rejected findings are read-only and must not drive Terraform import or cleanup automation",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		cloudRuntimeDriftReadbackCapability,
		TruthBasisSemanticFacts,
		"resolved from active reducer-materialized provider-neutral runtime drift findings "+
			"(reducer_multi_cloud_runtime_drift_finding)",
	))
}

func (h *CloudRuntimeDriftHandler) writeContractError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	message string,
	code ErrorCode,
) {
	WriteContractError(
		w, r, status, message, code,
		cloudRuntimeDriftReadbackCapability,
		h.profile(),
		requiredProfile(cloudRuntimeDriftReadbackCapability),
	)
}

// cloudRuntimeDriftFindingViews projects reducer rows into the bounded wire shape.
// Each view applies the shared safety gate so an unsafe finding is reported as
// rejected with a refused action rather than dropped, and the provider-neutral
// source state is resolved through the same taxonomy the AWS surface uses.
func cloudRuntimeDriftFindingViews(rows []MultiCloudRuntimeDriftFindingRow) []CloudRuntimeDriftFindingView {
	views := make([]CloudRuntimeDriftFindingView, 0, len(rows))
	for _, row := range rows {
		status := strings.TrimSpace(row.ManagementStatus)
		gate := iacManagementSafetyGate(status, row.WarningFlags, nil)
		views = append(views, CloudRuntimeDriftFindingView{
			FactID:                       row.FactID,
			Provider:                     strings.TrimSpace(row.Provider),
			ScopeID:                      row.ScopeID,
			GenerationID:                 row.GenerationID,
			SourceSystem:                 row.SourceSystem,
			CloudResourceUID:             row.CloudResourceUID,
			FindingKind:                  strings.TrimSpace(row.FindingKind),
			ManagementStatus:             status,
			Confidence:                   row.Confidence,
			SourceState:                  string(ResolveReplatformingSourceState(status, gate.ReviewRequired)),
			MatchedTerraformStateAddress: row.MatchedTerraformStateAddress,
			MissingEvidence:              row.MissingEvidence,
			RecommendedAction:            row.RecommendedAction,
			DriftedAttributes:            row.DriftedAttributes,
			SafetyGate:                   gate,
		})
	}
	return views
}

// cloudRuntimeDriftSourceStateGroup counts findings sharing one provider-neutral
// source state for a quick rollup, with canonical uids attached for drilldown.
type cloudRuntimeDriftSourceStateGroup struct {
	SourceState       string   `json:"source_state"`
	Count             int      `json:"count"`
	CloudResourceUIDs []string `json:"cloud_resource_uids,omitempty"`
}

// cloudRuntimeDriftSourceStateGroups rolls views up by provider-neutral source
// state in canonical order so callers can summarize refusal posture cheaply.
func cloudRuntimeDriftSourceStateGroups(views []CloudRuntimeDriftFindingView) []cloudRuntimeDriftSourceStateGroup {
	byState := map[string]*cloudRuntimeDriftSourceStateGroup{}
	var states []string
	for _, view := range views {
		group := byState[view.SourceState]
		if group == nil {
			group = &cloudRuntimeDriftSourceStateGroup{SourceState: view.SourceState}
			byState[view.SourceState] = group
			states = append(states, view.SourceState)
		}
		group.Count++
		group.CloudResourceUIDs = append(group.CloudResourceUIDs, view.CloudResourceUID)
	}
	sort.Strings(states)
	out := make([]cloudRuntimeDriftSourceStateGroup, 0, len(states))
	for _, state := range states {
		group := byState[state]
		sort.Strings(group.CloudResourceUIDs)
		out = append(out, *group)
	}
	return out
}

func cloudRuntimeDriftStory(
	filter MultiCloudRuntimeDriftFilter,
	views []CloudRuntimeDriftFindingView,
	total int,
) string {
	scope := filter.ScopeID
	if filter.Provider != "" {
		scope = filter.Provider + " resources in " + scope
	}
	return fmt.Sprintf(
		"%d active multi-cloud runtime drift findings matched %s; %d returned in this page.",
		total,
		scope,
		len(views),
	)
}

func cloudRuntimeDriftTruncated(offset, pageLen, total int) bool {
	return offset+pageLen < total
}

func cloudRuntimeDriftNextOffset(offset, pageLen, total int) int {
	if offset+pageLen < total {
		return offset + pageLen
	}
	return 0
}

// normalizeCloudRuntimeDriftRequest validates and bounds the readback request.
// It requires a canonical scope (scope_id or a provider-flavored alias), rejects
// unknown providers and finding kinds as invalid input, and caps the page size.
func normalizeCloudRuntimeDriftRequest(req cloudRuntimeDriftRequest) (MultiCloudRuntimeDriftFilter, error) {
	scope := cloudRuntimeDriftScope(req)
	if scope == "" {
		return MultiCloudRuntimeDriftFilter{}, fmt.Errorf("scope_id, account_id, project_id, or subscription_id is required")
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider != "" {
		if _, known := cloudRuntimeDriftProviders[provider]; !known {
			return MultiCloudRuntimeDriftFilter{}, fmt.Errorf("provider must be one of aws, gcp, or azure")
		}
	}
	kinds, err := normalizeIaCManagementFindingKinds(req.FindingKinds)
	if err != nil {
		return MultiCloudRuntimeDriftFilter{}, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = cloudRuntimeDriftDefaultLimit
	}
	if limit > cloudRuntimeDriftMaxLimit {
		limit = cloudRuntimeDriftMaxLimit
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	return MultiCloudRuntimeDriftFilter{
		ScopeID:          scope,
		Provider:         provider,
		CloudResourceUID: strings.TrimSpace(req.CloudResourceUID),
		FindingKinds:     kinds,
		Limit:            limit,
		Offset:           offset,
	}, nil
}

// cloudRuntimeDriftScope resolves the canonical scope from the request, accepting
// scope_id and the provider-flavored aliases account_id, project_id, and
// subscription_id. They all target the same canonical scope_id because a finding
// belongs to exactly one ingestion scope; the first non-empty alias wins.
func cloudRuntimeDriftScope(req cloudRuntimeDriftRequest) string {
	for _, value := range []string{req.ScopeID, req.AccountID, req.ProjectID, req.SubscriptionID} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
