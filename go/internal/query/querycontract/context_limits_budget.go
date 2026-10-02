// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "slices"

// contextEvidenceRowKeys are the deployment_evidence row lists the context
// routes cut to ContextStoryItemLimit. They are the same four lists the
// service story bounds in its raw evidence copy, so the two surfaces agree on
// what one page of deployment evidence holds (#7129).
var contextEvidenceRowKeys = []string{"artifacts", "delivery_paths", "delivery_workflows", "shared_config_paths"}

// capContextBudgetRows bounds the two payload families that kept the workload
// and service context responses over the MCP response budget after the
// hostname and entrypoint caps (#7129): the API surface endpoint rows, which
// the overview repeated, and the deployment-evidence row lists.
//
// It runs from WorkloadContextResultLimits, after every consumer of the full
// lists has read them, so the cut changes only what ships. Pre-cut totals stay
// on the existing count fields (api_surface.endpoint_count,
// deployment_evidence.artifact_count) and in deployment_evidence.raw_limits.
// Each cut, and each cut the graph read already made, is named on
// ctx["limitations"] so ContextPartialReasons promotes it to partial_reasons.
// Rows cut from deployment_evidence.artifacts stay addressable through
// evidence_index.*.resolved_ids and the get_relationship_evidence drilldown.
//
// It returns the artifact total before the cut and whether any of these
// families is truncated.
func capContextBudgetRows(ctx map[string]any) (artifactTotal int, truncated bool) {
	artifactTotal, evidenceTruncated := capContextDeploymentEvidence(ctx)
	apiTruncated := capContextAPISurface(ctx)
	infraTruncated := capContextInfrastructure(ctx)
	return artifactTotal, evidenceTruncated || apiTruncated || infraTruncated
}

// capContextInfrastructure cuts the infrastructure list to ContextStoryItemLimit
// and names the cut with a reason of its own. The read bound is 5,000 rows, and
// the 50-row context caps never covered this list, so a service shipped up to
// 5,000 rows (about 730 KB). infrastructure_truncated is a different signal:
// it means the read itself hit that 5,000-row bound.
func capContextInfrastructure(ctx map[string]any) bool {
	rows := MapSliceValue(ctx, "infrastructure")
	capped, cut := CapMapRows(rows, ContextStoryItemLimit)
	if !cut {
		return false
	}
	ctx["infrastructure"] = capped
	appendContextLimitation(ctx, "infrastructure_rows_truncated")
	return true
}

// capContextDeploymentEvidence cuts the deployment_evidence row lists on a
// copy of the evidence map, so a map shared with a read model is never
// mutated.
func capContextDeploymentEvidence(ctx map[string]any) (artifactTotal int, truncated bool) {
	evidence := MapValue(ctx, "deployment_evidence")
	artifactTotal = len(MapSliceValue(evidence, "artifacts"))
	if len(evidence) == 0 {
		return artifactTotal, false
	}
	var shaped map[string]any
	rawLimits := map[string]any{}
	for _, key := range contextEvidenceRowKeys {
		rows := MapSliceValue(evidence, key)
		capped, cut := CapMapRows(rows, ContextStoryItemLimit)
		if !cut {
			continue
		}
		if shaped == nil {
			shaped = CopyMap(evidence)
		}
		shaped[key] = capped
		rawLimits[key] = map[string]any{
			"count":     len(rows),
			"limit":     ContextStoryItemLimit,
			"truncated": true,
		}
		appendContextLimitation(ctx, "deployment_evidence_"+key+"_truncated")
		truncated = true
	}
	// The graph read cuts each direction at its own limit before this runs and
	// reports it as artifacts_truncated; promote that so the cut is not silent.
	if BoolVal(evidence, "artifacts_truncated") {
		appendContextLimitation(ctx, "deployment_evidence_artifacts_truncated")
		truncated = true
	}
	if shaped == nil {
		return artifactTotal, truncated
	}
	if _, cutArtifacts := rawLimits["artifacts"]; cutArtifacts {
		shaped["artifacts_truncated"] = true
	}
	shaped["raw_limits"] = rawLimits
	ctx["deployment_evidence"] = shaped
	return artifactTotal, truncated
}

// capContextAPISurface cuts api_surface.endpoints to ContextStoryItemLimit and
// ships the endpoint rows once. deployment_overview carries a second copy of
// the same rows; it keeps the counts and names where the rows are.
func capContextAPISurface(ctx map[string]any) bool {
	surface := MapValue(ctx, "api_surface")
	endpoints := MapSliceValue(surface, "endpoints")
	if len(endpoints) == 0 {
		return false
	}
	truncated := BoolVal(surface, "detail_truncated")
	if capped, cut := CapMapRows(endpoints, ContextStoryItemLimit); cut {
		shaped := CopyMap(surface)
		shaped["endpoints"] = capped
		shaped["detail_limit"] = ContextStoryItemLimit
		shaped["detail_truncated"] = true
		ctx["api_surface"] = shaped
		truncated = true
	}
	if truncated {
		appendContextLimitation(ctx, "api_surface_endpoints_truncated")
	}
	dropContextOverviewEndpoints(ctx)
	return truncated
}

// dropContextOverviewEndpoints removes the endpoint rows from
// deployment_overview.api_surface. The same rows ship at the top level, where
// the cap applies to them; the overview keeps its counts and a pointer.
func dropContextOverviewEndpoints(ctx map[string]any) {
	overview := MapValue(ctx, "deployment_overview")
	overviewSurface := MapValue(overview, "api_surface")
	if _, has := overviewSurface["endpoints"]; !has {
		return
	}
	shapedSurface := CopyMap(overviewSurface)
	delete(shapedSurface, "endpoints")
	shapedSurface["endpoints_shipped_at"] = "api_surface.endpoints"
	shapedOverview := CopyMap(overview)
	shapedOverview["api_surface"] = shapedSurface
	ctx["deployment_overview"] = shapedOverview
}

// appendContextLimitation records reason on ctx["limitations"] once.
func appendContextLimitation(ctx map[string]any, reason string) {
	limitations := StringSliceVal(ctx, "limitations")
	if !slices.Contains(limitations, reason) {
		ctx["limitations"] = append(limitations, reason)
	}
}
