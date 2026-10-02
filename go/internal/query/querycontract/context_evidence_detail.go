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
// (a regrouping of the same artifacts that handle rows already carry), and
// returns one truth omission per reduced family with its true total. Rows are
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
	if omission, ok := projectContextEndpoints(ctx); ok {
		omissions = append(omissions, omission)
	}
	if len(omissions) == 0 {
		return nil
	}
	ctx["evidence_detail_drilldown"] = map[string]any{
		"full_rows":     "repeat the call with evidence_detail full; rows past the 50 shipped are listed by resolved_id only under full",
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
