// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// sections.go shapes an already-built trace_deployment_chain response by the
// caller's section selection and evidence detail (#7174). Every family is
// fully built first, so overview counts, the story, and the fact summary read
// the full lists; this file only decides what ships (the #7169 emission-time
// cut). It issues no reads and does not change any per-family query cap.

// Evidence detail modes for the trace_deployment_chain response.
const (
	// EvidenceDetailFull emits every selected family with its full rows. It is
	// the HTTP default, so an HTTP body without evidence_detail keeps today's
	// response.
	EvidenceDetailFull = "full"
	// EvidenceDetailHandles emits the selected primary families as handle
	// rows (a projection of each row's identity keys) and, when no sections
	// are named, omits the families derived from them. It is the MCP default.
	EvidenceDetailHandles = "handles"
)

const (
	sectionDetailOmitted = "omitted"
	sectionDrilldownTool = "trace_deployment_chain"
)

// ErrInvalidSectionSelection is wrapped by SectionSelection.Validate when the
// caller names an unknown evidence detail or section. Handlers map it to a
// 400 invalid_argument.
var ErrInvalidSectionSelection = errors.New("invalid trace_deployment_chain section selection")

// traceSection is one selectable family of the trace response.
type traceSection struct {
	name string
	// keys are the response keys the family owns (overviews owns several).
	keys []string
	// handleKeys, when set, projects each row of the family's list to these
	// keys under EvidenceDetailHandles.
	handleKeys []string
	// project, when set, is the family's non-list handle projection.
	project func(response map[string]any)
	// count returns the rows the family holds in response.
	count func(response map[string]any) int
	// derived families are pure derivations of a primary family and are
	// omitted by the handles default.
	derived bool
}

func (s traceSection) projects() bool { return s.handleKeys != nil || s.project != nil }

func listSection(name string, handleKeys ...string) traceSection {
	return traceSection{name: name, keys: []string{name}, handleKeys: handleKeys, count: listCount(name)}
}

func derivedSection(name string) traceSection {
	return traceSection{name: name, keys: []string{name}, count: listCount(name), derived: true}
}

func listCount(key string) func(map[string]any) int {
	return func(response map[string]any) int { return len(querycontract.MapSliceValue(response, key)) }
}

// overviewKeys are the response keys the "overviews" family owns.
var overviewKeys = []string{
	"story_sections", "deployment_overview", "controller_overview", "gitops_overview",
	"runtime_overview", "provenance_overview", "documentation_overview", "support_overview",
}

// traceSections is the single table behind SectionNames, section_detail, and
// truth.omissions. Handle keys are the real row identity keys verified
// against each producer.
var traceSections = []traceSection{
	listSection("instances", "instance_id", "environment", "platform_name", "platform_kind"),
	derivedSection("topology_edges"),
	listSection("provisioned_platforms", "platform_id", "platform_name", "platform_kind"),
	listSection("deployment_sources", "repo_id", "repo_name", "relationship_type"),
	listSection("cloud_resources", "id", "name", "kind"),
	listSection("uncorrelated_cloud_resources", "id", "name", "kind"),
	listSection("k8s_resources", "entity_id", "entity_name", "kind"),
	derivedSection("k8s_relationships"),
	listSection("image_registry_truth", "image_ref", "digest", "match_strength"),
	derivedSection("deployment_facts"),
	derivedSection("controller_driven_paths"),
	derivedSection("delivery_paths"),
	{
		name: "deployment_evidence", keys: []string{"deployment_evidence"},
		project: projectDeploymentEvidence,
		count:   countDeploymentEvidence,
	},
	derivedSection("artifact_lineage"),
	listSection("hostnames", "hostname", "environment"),
	derivedSection("entrypoints"),
	derivedSection("network_paths"),
	{
		name: "api_surface", keys: []string{"api_surface"},
		project: func(response map[string]any) { dropAPIEndpoints(response, "api_surface") },
		count: func(response map[string]any) int {
			return len(querycontract.MapSliceValue(querycontract.MapValue(response, "api_surface"), "endpoints"))
		},
	},
	listSection("dependents", "repository", "repo_id"),
	listSection("consumer_repositories", "repository", "repo_id"),
	listSection("provisioning_source_chains", "repository", "repo_id"),
	{
		name: "story", keys: []string{"story"},
		count: func(response map[string]any) int {
			if querycontract.StringVal(response, "story") == "" {
				return 0
			}
			return 1
		},
	},
	{
		name: "overviews", keys: overviewKeys, project: projectOverviews,
		count: func(response map[string]any) int {
			present := 0
			for _, key := range overviewKeys {
				if _, ok := response[key]; ok {
					present++
				}
			}
			return present
		},
	},
}

// SectionNames returns the selectable trace_deployment_chain family names in
// response order. It is the single source for the request enum in the HTTP
// handler, the OpenAPI fragment, and the MCP input schema; every other
// response key (identity, image_refs, deployment_fact_summary, *_limits,
// *_truncated, drilldowns, evidence_boundaries, evidence_detail,
// section_detail) is always emitted.
func SectionNames() []string {
	names := make([]string, 0, len(traceSections))
	for _, section := range traceSections {
		names = append(names, section.name)
	}
	return names
}

// EvidenceDetailValues returns the accepted evidence_detail values.
func EvidenceDetailValues() []string {
	return []string{EvidenceDetailFull, EvidenceDetailHandles}
}

// SectionSelection is the caller's choice of which families the trace
// response emits (Sections) and how their rows are shaped (EvidenceDetail).
// The zero value is today's full response.
type SectionSelection struct {
	// EvidenceDetail is "full", "handles", or empty (meaning "full").
	EvidenceDetail string
	// Sections names the families to emit. Nil or empty means the mode's
	// default set: every family under "full", and every non-derived family
	// under "handles".
	Sections []string
	// Replay holds the request arguments that decide which rows exist
	// (direct_only, max_depth, include_related_module_usage). Every
	// drilldown_arguments entry carries them, so replaying a drilldown builds
	// the same families the original call did instead of the adapter's
	// defaults (the MCP default direct_only=true would skip the consumer and
	// provisioning families). A nil Replay adds nothing.
	Replay map[string]any
}

// Validate returns an error wrapping ErrInvalidSectionSelection that names
// the allowed values when EvidenceDetail or any Sections entry is unknown.
func (s SectionSelection) Validate() error {
	switch s.EvidenceDetail {
	case "", EvidenceDetailFull, EvidenceDetailHandles:
	default:
		return fmt.Errorf("%w: evidence_detail %q is not one of %s",
			ErrInvalidSectionSelection, s.EvidenceDetail, strings.Join(EvidenceDetailValues(), ", "))
	}
	known := make(map[string]struct{}, len(traceSections))
	for _, section := range traceSections {
		known[section.name] = struct{}{}
	}
	for _, name := range s.Sections {
		if _, ok := known[name]; !ok {
			return fmt.Errorf("%w: section %q is not one of %s",
				ErrInvalidSectionSelection, name, strings.Join(SectionNames(), ", "))
		}
	}
	return nil
}

// ApplySectionSelection shapes a response built by
// BuildDeploymentTraceResponse in place: it deletes unselected families,
// projects selected families to handle rows under "handles", and sets
// evidence_detail plus section_detail (detail, returned, total, and a
// trace_deployment_chain drilldown for every family not returned in full).
// It returns the same non-full families as truth omissions; the result is
// empty when every family ships in full. Rows are projected into new maps,
// so the workload context the response shares rows with is never mutated.
// Call Validate first; unknown section names are ignored here.
func ApplySectionSelection(response map[string]any, sel SectionSelection) []querycontract.TruthOmission {
	detail := sel.EvidenceDetail
	if detail == "" {
		detail = EvidenceDetailFull
	}
	var selected map[string]bool
	if len(sel.Sections) > 0 {
		selected = make(map[string]bool, len(sel.Sections))
		for _, name := range sel.Sections {
			selected[name] = true
		}
	}
	serviceName := querycontract.SafeStr(response, "service_name")
	sectionDetail := make(map[string]any, len(traceSections))
	var omissions []querycontract.TruthOmission
	for _, section := range traceSections {
		total := section.count(response)
		state := EvidenceDetailFull
		switch {
		case !sectionEmitted(section, detail, selected):
			state = sectionDetailOmitted
			for _, key := range section.keys {
				delete(response, key)
			}
		case detail == EvidenceDetailHandles && section.projects():
			state = EvidenceDetailHandles
			projectSection(response, section)
		}
		entry := map[string]any{"detail": state, "returned": section.count(response), "total": total}
		if state != EvidenceDetailFull {
			entry["drilldown_tool"] = sectionDrilldownTool
			arguments := map[string]any{
				"service_name":    serviceName,
				"sections":        []string{section.name},
				"evidence_detail": EvidenceDetailFull,
			}
			for key, value := range sel.Replay {
				arguments[key] = value
			}
			entry["drilldown_arguments"] = arguments
			omissions = append(omissions, querycontract.TruthOmission{Section: section.name, Detail: state, Total: total})
		}
		sectionDetail[section.name] = entry
	}
	response["evidence_detail"] = detail
	response["section_detail"] = sectionDetail
	return omissions
}

func sectionEmitted(section traceSection, detail string, selected map[string]bool) bool {
	if selected != nil {
		return selected[section.name]
	}
	return detail != EvidenceDetailHandles || !section.derived
}

func projectSection(response map[string]any, section traceSection) {
	if section.project != nil {
		section.project(response)
		return
	}
	if rows, ok := response[section.name].([]map[string]any); ok {
		response[section.name] = projectRows(rows, section.handleKeys)
	}
}

// projectRows copies each row's handle keys into a new row. Keys absent from
// a row stay absent, so a handle never asserts a value the full row lacks.
func projectRows(rows []map[string]any, keys []string) []map[string]any {
	projected := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		handle := make(map[string]any, len(keys))
		for _, key := range keys {
			if value, ok := row[key]; ok {
				handle[key] = value
			}
		}
		projected = append(projected, handle)
	}
	return projected
}

// contentDerivedEvidenceKeys are the deployment_evidence lists the service
// enrichment builds from repository content when the graph holds no evidence
// (service.buildServiceDeploymentEvidenceFromOverview). They come from up to
// RepositorySemanticEntityLimit files with no row cap of their own, so handles
// mode drops them and section_detail counts them.
var contentDerivedEvidenceKeys = []string{
	"deployment_artifacts",
	"shared_config_paths",
	"delivery_paths",
	"delivery_family_paths",
	"delivery_family_story",
	"delivery_workflows",
	"topology_story",
}

// countDeploymentEvidence counts the rows deployment_evidence would ship: the
// graph artifacts plus every content-derived list still present. Scalars such
// as a story sentence count as zero rows.
func countDeploymentEvidence(response map[string]any) int {
	evidence := querycontract.MapValue(response, "deployment_evidence")
	count := len(querycontract.MapSliceValue(evidence, "artifacts"))
	for _, key := range contentDerivedEvidenceKeys {
		count += listLen(evidence[key])
	}
	return count
}

// listLen returns the length of a slice value and 0 for anything else.
func listLen(value any) int {
	if value == nil {
		return 0
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice {
		return 0
	}
	return rv.Len()
}

// projectDeploymentEvidence keeps deployment_evidence's counts and family
// lists, projects artifacts to {id, relationship_type, resolved_id}, drops
// evidence_index (a regrouping of the same artifacts), and drops the
// content-derived lists (see contentDerivedEvidenceKeys), which section_detail
// still counts. A scalar story is small and stays.
func projectDeploymentEvidence(response map[string]any) {
	evidence, ok := response["deployment_evidence"].(map[string]any)
	if !ok {
		return
	}
	shaped := copyMap(evidence)
	delete(shaped, "evidence_index")
	for _, key := range contentDerivedEvidenceKeys {
		if listLen(shaped[key]) > 0 {
			delete(shaped, key)
		}
	}
	if artifacts := querycontract.MapSliceValue(evidence, "artifacts"); artifacts != nil {
		shaped["artifacts"] = projectRows(artifacts, []string{"id", "relationship_type", "resolved_id"})
	}
	response["deployment_evidence"] = shaped
}

// projectOverviews projects controller_overview.entities to handle rows and
// drops the endpoint list from the api_surface copy that the service-story
// builder places on deployment_overview. Every count stays as computed from
// the full lists.
func projectOverviews(response map[string]any) {
	if overview, ok := response["controller_overview"].(map[string]any); ok {
		if entities := querycontract.MapSliceValue(overview, "entities"); entities != nil {
			shaped := copyMap(overview)
			shaped["entities"] = projectRows(entities, []string{"entity_id", "entity_name", "controller_kind"})
			response["controller_overview"] = shaped
		}
	}
	if overview, ok := response["deployment_overview"].(map[string]any); ok {
		if _, has := overview["api_surface"]; has {
			shaped := copyMap(overview)
			dropAPIEndpoints(shaped, "api_surface")
			response["deployment_overview"] = shaped
		}
	}
}

// dropAPIEndpoints replaces container[key] with a copy of the api_surface map
// that keeps its counts and omits the endpoint rows.
func dropAPIEndpoints(container map[string]any, key string) {
	surface, ok := container[key].(map[string]any)
	if !ok {
		return
	}
	shaped := copyMap(surface)
	delete(shaped, "endpoints")
	container[key] = shaped
}

func copyMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
