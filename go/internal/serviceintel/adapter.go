// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// FromServiceStory maps a get_service_story dossier response into the report
// ReportInput it sources: the subject identity plus the identity,
// code_to_runtime, and deployment_config sections. It is a faithful,
// side-effect-free translation — it reads only confirmed dossier fields and
// section cardinalities, never invents evidence, and carries the source truth
// envelope verbatim onto each section. Callers append supply-chain and incident
// sections from their own routes before calling Compose.
//
// A nil truth marks every section unsupported, reflecting a dossier that could
// not be classified rather than fabricating confidence.
func FromServiceStory(dossier map[string]any, truth *query.TruthEnvelope) ReportInput {
	if dossier == nil {
		return ReportInput{}
	}
	identity := subMap(dossier, "service_identity")
	subject := ReportSubject{
		ServiceID:   query.StringVal(identity, "service_id"),
		ServiceName: query.StringVal(identity, "service_name"),
		RepoID:      query.StringVal(identity, "repo_id"),
		RepoName:    query.StringVal(identity, "repo_name"),
	}
	truncated := query.BoolVal(subMap(dossier, "result_limits"), "truncated")
	serviceHandle, hasHandle := serviceEntityHandle(subject)

	entrypoints := sliceLen(dossier, "entrypoints")
	networkPaths := sliceLen(dossier, "network_paths")
	// The service-story builder always emits api_surface, and the code-to-runtime
	// trace's entrypoints segment is populated from api_surface.endpoints, so an
	// API-spec-only service still has real code-to-runtime evidence even with no
	// raw entrypoints or network paths.
	apiEndpoints := apiSurfaceEndpointCount(dossier)
	codeToRuntime := entrypoints > 0 || networkPaths > 0 || apiEndpoints > 0
	lanes := sliceLen(dossier, "deployment_lanes")

	input := ReportInput{Subject: subject}
	input.Sections = []SectionInput{
		{
			Kind:        SectionIdentity,
			Summary:     identitySummary(subject, query.StringVal(identity, "kind")),
			Truth:       truth,
			Evidence:    handlesIf(hasHandle, serviceHandle),
			Limitations: query.StringSliceVal(identity, "limitations"),
			Truncated:   truncated,
			NoEvidence:  !hasHandle,
		},
		{
			Kind:       SectionCodeToRuntime,
			Summary:    codeToRuntimeSummary(subject, entrypoints, networkPaths, apiEndpoints),
			Truth:      truth,
			Evidence:   handlesIf(hasHandle && codeToRuntime, serviceHandle),
			Truncated:  truncated,
			NoEvidence: !codeToRuntime,
		},
		{
			Kind:       SectionDeploymentConfig,
			Summary:    deploymentSummary(subject, lanes),
			Truth:      truth,
			Evidence:   handlesIf(hasHandle && lanes > 0, serviceHandle),
			Truncated:  truncated,
			NoEvidence: lanes == 0,
		},
	}
	return input
}

// FromSupplyChainInventory maps a get_supply_chain_impact_inventory response
// into the report's supply_chain SectionInput. It is faithful and side-effect
// free: it reads only the confirmed top-level `count` and `truncated` fields,
// carries the source truth envelope verbatim, addresses the section with the
// service entity handle when a subject is known, and marks the section
// NoEvidence when the inventory is empty so Compose keeps it visible with a
// fallback next call. A nil dossier yields a zero SectionInput the caller can
// skip.
func FromSupplyChainInventory(inventory map[string]any, subject ReportSubject, truth *query.TruthEnvelope) SectionInput {
	if inventory == nil {
		return SectionInput{}
	}
	count := query.IntVal(inventory, "count")
	truncated := query.BoolVal(inventory, "truncated")
	hasEvidence := count > 0
	handle, hasHandle := serviceEntityHandle(subject)
	return SectionInput{
		Kind:       SectionSupplyChain,
		Summary:    supplyChainSummary(subject, count),
		Truth:      truth,
		Evidence:   handlesIf(hasHandle && hasEvidence, handle),
		Truncated:  truncated,
		NoEvidence: !hasEvidence,
	}
}

func supplyChainSummary(subject ReportSubject, count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("Service %s has %d supply-chain impact finding(s).", subjectName(subject), count)
}

// IncidentRecord is the minimal durable, service-scoped incident evidence the
// report's incidents_support section needs. Callers map their source records (for
// example the durable reducer.ServiceIncidentRecord loaded via the
// incident-repository correlation join) onto it, so this package stays decoupled
// from the storage and reducer layers and never reclassifies the join.
type IncidentRecord struct {
	// Provider is the incident provider (for example pagerduty).
	Provider string
	// ProviderIncidentID is the durable provider incident identifier.
	ProviderIncidentID string
	// TruthLabel is the source truth label for the routing evidence.
	TruthLabel string
}

// maxReportIncidents bounds the incidents surfaced in one report so a service
// with a long incident history yields a scannable, bounded section rather than
// an unbounded handle list; overflow is signalled via the section truncation.
const maxReportIncidents = 50

// FromIncidentEvidence maps durable service-scoped incident evidence into the
// incidents_support SectionInput. It is faithful and side-effect free: it counts
// distinct incidents (the loader returns one row per evidence slot), addresses
// each with an entity handle keyed by the durable provider incident id, carries
// the source truth verbatim, bounds the set to maxReportIncidents (marking the
// section truncated on overflow), and marks NoEvidence when the service has no
// routed incidents. Callers invoke it only when the durable incident source is
// wired; an unwired source leaves the section unsupported with its fallback.
func FromIncidentEvidence(records []IncidentRecord, subject ReportSubject, truth *query.TruthEnvelope) SectionInput {
	incidents := distinctIncidents(records)
	truncated := len(incidents) > maxReportIncidents
	if truncated {
		incidents = incidents[:maxReportIncidents]
	}
	return SectionInput{
		Kind:       SectionIncidentsSupport,
		Summary:    incidentSummary(subject, len(incidents), truncated),
		Truth:      truth,
		Evidence:   incidentHandles(incidents),
		Truncated:  truncated,
		NoEvidence: len(incidents) == 0,
	}
}

// distinctIncidents returns unique incidents keyed by (provider, provider incident
// id) in stable input order, so two providers that share an id string are not
// merged and the count reflects incidents, not evidence slots.
func distinctIncidents(records []IncidentRecord) []IncidentRecord {
	type incidentKey struct{ provider, id string }
	seen := make(map[incidentKey]struct{}, len(records))
	var out []IncidentRecord
	for _, record := range records {
		id := strings.TrimSpace(record.ProviderIncidentID)
		if id == "" {
			continue
		}
		key := incidentKey{provider: strings.TrimSpace(record.Provider), id: id}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, IncidentRecord{Provider: key.provider, ProviderIncidentID: id, TruthLabel: record.TruthLabel})
	}
	return out
}

func incidentHandles(incidents []IncidentRecord) []query.EvidenceCitationHandle {
	if len(incidents) == 0 {
		return nil
	}
	handles := make([]query.EvidenceCitationHandle, 0, len(incidents))
	for _, incident := range incidents {
		reason := "incident routed to the service via the durable incident-repository correlation"
		if incident.Provider != "" {
			reason = incident.Provider + " " + reason
		}
		handles = append(handles, query.EvidenceCitationHandle{
			Kind:           "entity",
			EntityID:       incident.ProviderIncidentID,
			EvidenceFamily: "incident_routing",
			Reason:         reason,
		})
	}
	return handles
}

func incidentSummary(subject ReportSubject, count int, truncated bool) string {
	if count == 0 {
		return ""
	}
	if truncated {
		return fmt.Sprintf("Service %s has at least %d routed incident(s) (truncated).", subjectName(subject), count)
	}
	return fmt.Sprintf("Service %s has %d routed incident(s).", subjectName(subject), count)
}

// serviceEntityHandle builds the evidence handle that addresses the service node
// itself: the canonical, resolvable pointer behind a service-story claim (the
// graph node is the evidence). It emits an `entity` handle keyed by the service
// id, the only service-level kind the evidence-citation normalizer accepts, so a
// caller can follow the report's handles into build_evidence_citation_packet
// without a bad-request. It returns ok=false when there is no service id, so the
// adapter never emits a handle the citation surface would reject.
func serviceEntityHandle(subject ReportSubject) (query.EvidenceCitationHandle, bool) {
	entityID := strings.TrimSpace(subject.ServiceID)
	if entityID == "" {
		return query.EvidenceCitationHandle{}, false
	}
	return query.EvidenceCitationHandle{
		Kind:           "entity",
		EntityID:       entityID,
		EvidenceFamily: "service_story",
		Reason:         "service identity resolved from the service story dossier",
	}, true
}

// apiSurfaceEndpointCount returns the number of API-surface endpoints the
// dossier carries, preferring the builder's endpoint_count and falling back to
// the endpoints slice length. It reads only confirmed api_surface fields.
func apiSurfaceEndpointCount(dossier map[string]any) int {
	apiSurface := subMap(dossier, "api_surface")
	if apiSurface == nil {
		return 0
	}
	if count := query.IntVal(apiSurface, "endpoint_count"); count > 0 {
		return count
	}
	return sliceLen(apiSurface, "endpoints")
}

func identitySummary(subject ReportSubject, kind string) string {
	name := subjectName(subject)
	if k := strings.TrimSpace(kind); k != "" {
		return fmt.Sprintf("Service %s is a %s.", name, k)
	}
	return fmt.Sprintf("Service %s.", name)
}

func codeToRuntimeSummary(subject ReportSubject, entrypoints, networkPaths, apiEndpoints int) string {
	if entrypoints == 0 && networkPaths == 0 && apiEndpoints == 0 {
		return ""
	}
	return fmt.Sprintf("Service %s exposes %d entrypoint(s) and %d API endpoint(s) over %d evidence-backed network path(s).",
		subjectName(subject), entrypoints, apiEndpoints, networkPaths)
}

func deploymentSummary(subject ReportSubject, lanes int) string {
	if lanes == 0 {
		return ""
	}
	return fmt.Sprintf("Service %s deploys across %d evidence-backed lane(s).", subjectName(subject), lanes)
}

// handlesIf returns a single-handle slice when the condition holds, else nil, so
// a section carries evidence only when the dossier actually supports it.
func handlesIf(ok bool, handle query.EvidenceCitationHandle) []query.EvidenceCitationHandle {
	if !ok {
		return nil
	}
	return []query.EvidenceCitationHandle{handle}
}

// subMap returns m[key] as a map[string]any, or nil when absent or another type.
func subMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if nested, ok := m[key].(map[string]any); ok {
		return nested
	}
	return nil
}

// sliceLen returns the length of m[key] whether it decoded as []any (JSON) or
// []map[string]any (in-process), and 0 otherwise. It reads only cardinality, so
// it never depends on element field shapes.
func sliceLen(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch value := m[key].(type) {
	case []any:
		return len(value)
	case []map[string]any:
		return len(value)
	default:
		return 0
	}
}
