// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"fmt"
	"strings"
)

// Evidence detail modes for the workload and service context routes (#7129).
const (
	// ContextEvidenceDetailFull returns every deployment-evidence artifact row
	// and API surface endpoint row in full. It is the HTTP default, so an HTTP
	// body without evidence_detail keeps today's shape.
	ContextEvidenceDetailFull = "full"
	// ContextEvidenceDetailHandles projects those rows to their identity keys.
	// The MCP context tools default to it so a populated service fits the
	// dispatch response budget; every count stays computed from the full rows.
	ContextEvidenceDetailHandles = "handles"
)

// ErrInvalidContextEvidenceDetail is wrapped by ValidateContextEvidenceDetail
// when evidence_detail is neither full nor handles.
var ErrInvalidContextEvidenceDetail = errors.New("invalid context evidence_detail")

// contextArtifactHandleKeys and contextEndpointHandleKeys are the identity keys
// handles mode keeps. The artifact keys match the trace_deployment_chain
// handle rows, so one drilldown (get_relationship_evidence by resolved_id)
// serves both tools.
var (
	contextArtifactHandleKeys = []string{"id", "relationship_type", "resolved_id"}
	contextEndpointHandleKeys = []string{"id", "path", "methods"}
)

// ValidateContextEvidenceDetail returns an error wrapping
// ErrInvalidContextEvidenceDetail that names the accepted values when detail
// is not empty, "full", or "handles".
func ValidateContextEvidenceDetail(detail string) error {
	switch detail {
	case "", ContextEvidenceDetailFull, ContextEvidenceDetailHandles:
		return nil
	default:
		return fmt.Errorf("%w: %q is not one of %s",
			ErrInvalidContextEvidenceDetail, detail,
			strings.Join([]string{ContextEvidenceDetailFull, ContextEvidenceDetailHandles}, ", "))
	}
}

// ApplyContextEvidenceDetail shapes a workload or service context payload in
// place after result_limits and the row caps have run, so every count and
// limit is computed from the full rows and the shaping changes only what ships.
//
// Under handles it projects deployment_evidence.artifacts and
// api_surface.endpoints to identity rows, drops deployment_evidence.evidence_index
// (a regrouping of the same artifacts that handle rows already carry) and the
// content-derived evidence values that hold rows (contextContentEvidenceKeys),
// and returns one truth omission per reduced family with its true total. Rows are
// projected into new maps, so a map shared with a read model is never mutated.
// An empty detail means full. It always sets evidence_detail; it adds
// evidence_detail_drilldown only when a family was reduced. Call
// ValidateContextEvidenceDetail first.
func ApplyContextEvidenceDetail(ctx map[string]any, detail string) []TruthOmission {
	if detail == "" {
		detail = ContextEvidenceDetailFull
	}
	ctx["evidence_detail"] = detail
	if detail != ContextEvidenceDetailHandles {
		return nil
	}
	var omissions []TruthOmission
	if omission, ok := projectContextArtifacts(ctx); ok {
		omissions = append(omissions, omission)
	}
	omissions = append(omissions, dropContextContentEvidence(ctx)...)
	if omission, ok := projectContextEndpoints(ctx); ok {
		omissions = append(omissions, omission)
	}
	if len(omissions) == 0 {
		return nil
	}
	ctx["evidence_detail_drilldown"] = map[string]any{
		"full_rows":     "repeat the call with evidence_detail full; under handles the evidence_index is dropped, so rows past the 50 shipped need full",
		"artifact_tool": "get_relationship_evidence",
		"artifact_key":  "resolved_id",
	}
	return omissions
}

func projectContextArtifacts(ctx map[string]any) (TruthOmission, bool) {
	evidence := MapValue(ctx, "deployment_evidence")
	artifacts := MapSliceValue(evidence, "artifacts")
	if len(artifacts) == 0 {
		return TruthOmission{}, false
	}
	shaped := CopyMap(evidence)
	shaped["artifacts"] = projectContextRows(artifacts, contextArtifactHandleKeys)
	delete(shaped, "evidence_index")
	ctx["deployment_evidence"] = shaped
	total := max(len(artifacts), IntVal(MapValue(ctx, "result_limits"), "artifact_count"))
	return TruthOmission{Section: "deployment_evidence.artifacts", Detail: ContextEvidenceDetailHandles, Total: total}, true
}

func projectContextEndpoints(ctx map[string]any) (TruthOmission, bool) {
	surface := MapValue(ctx, "api_surface")
	endpoints := MapSliceValue(surface, "endpoints")
	if len(endpoints) == 0 {
		return TruthOmission{}, false
	}
	shaped := CopyMap(surface)
	shaped["endpoints"] = projectContextRows(endpoints, contextEndpointHandleKeys)
	ctx["api_surface"] = shaped
	total := max(len(endpoints), IntVal(surface, "endpoint_count"))
	return TruthOmission{Section: "api_surface.endpoints", Detail: ContextEvidenceDetailHandles, Total: total}, true
}

// contextContentEvidenceKeys are the deployment_evidence values built from
// repository content. Several come from up to a repository's semantic entity
// limit of files with no row cap of their own, so handles mode drops them and
// counts them in truth.omissions, the same rule trace_deployment_chain applies.
var contextContentEvidenceKeys = []string{
	"deployment_artifacts",
	"shared_config_paths",
	"delivery_paths",
	"delivery_family_paths",
	"delivery_family_story",
	"delivery_workflows",
	"topology_story",
	"relationship_overview",
}

// dropContextContentEvidence removes the content-derived values that hold rows
// from a copy of deployment_evidence and returns one omitted-section entry per
// value with the rows it held. A list the row cap already cut reports its
// pre-cut count from raw_limits. A value that holds no rows stays.
func dropContextContentEvidence(ctx map[string]any) []TruthOmission {
	evidence := MapValue(ctx, "deployment_evidence")
	if len(evidence) == 0 {
		return nil
	}
	var (
		shaped    map[string]any
		omissions []TruthOmission
	)
	rawLimits := MapValue(evidence, "raw_limits")
	for _, key := range contextContentEvidenceKeys {
		value, present := evidence[key]
		if !present {
			continue
		}
		rows := contextEvidenceRowCount(key, value)
		rows = max(rows, IntVal(MapValue(rawLimits, key), "count"))
		if rows == 0 {
			continue
		}
		if shaped == nil {
			shaped = CopyMap(evidence)
		}
		delete(shaped, key)
		omissions = append(omissions, TruthOmission{Section: "deployment_evidence." + key, Detail: contextEvidenceOmitted, Total: rows})
	}
	if shaped != nil {
		ctx["deployment_evidence"] = shaped
	}
	return omissions
}

// contextEvidenceOmitted is the TruthOmission detail for a section whose key is
// absent from the response.
const contextEvidenceOmitted = "omitted"

// contextEvidenceRowCount returns the rows a content-derived value holds: the
// length of a list, the summed lengths of the lists in a map, and for
// relationship_overview its relationship_count (its partition lists repeat the
// same rows, so summing them would count each twice).
func contextEvidenceRowCount(key string, value any) int {
	if key == "relationship_overview" {
		overview, _ := value.(map[string]any)
		if count := IntVal(overview, "relationship_count"); count > 0 {
			return count
		}
		return contextEvidenceRowCount("", overview["relationships"])
	}
	switch typed := value.(type) {
	case []map[string]any:
		return len(typed)
	case []string:
		return len(typed)
	case []any:
		return len(typed)
	case map[string]any:
		total := 0
		for _, inner := range typed {
			total += contextEvidenceRowCount("", inner)
		}
		return total
	default:
		return 0
	}
}

// projectContextRows returns a new row list holding only the named keys each
// row has. A key a row lacks stays absent rather than becoming a null.
func projectContextRows(rows []map[string]any, keys []string) []map[string]any {
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
