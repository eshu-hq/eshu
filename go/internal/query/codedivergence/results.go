// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

// ResultMap renders one assembled finding as the read surface's result
// shape: members with file and line range plus owning package, reasons,
// score, weakest-edge confidence for the graph kinds, and the cohort
// evidence block for the convention_outlier kind only.
func ResultMap(finding Finding) map[string]any {
	members := make([]map[string]any, 0, len(finding.Members))
	for _, member := range finding.Members {
		members = append(members, map[string]any{
			"entity_id":     member.EntityID,
			"entity_name":   member.EntityName,
			"entity_type":   member.EntityType,
			"file_path":     member.RelativePath,
			"relative_path": member.RelativePath,
			"repo_id":       finding.RepoID,
			"language":      member.Language,
			"package":       PackageOf(member.RelativePath),
			"start_line":    member.StartLine,
			"end_line":      member.EndLine,
			"token_count":   member.TokenCount,
			"source_handle": map[string]any{
				"repo_id":        finding.RepoID,
				"file_path":      member.RelativePath,
				"relative_path":  member.RelativePath,
				"start_line":     member.StartLine,
				"end_line":       member.EndLine,
				"entity_id":      member.EntityID,
				"content_tool":   "get_file_lines",
				"drilldown_tool": "get_entity_context",
			},
		})
	}
	reasons := make([]map[string]any, 0, len(finding.Reasons))
	for _, reason := range finding.Reasons {
		reasons = append(reasons, map[string]any{
			"code":     reason.Code,
			"sentence": reason.Sentence,
			"value":    reason.Value,
		})
	}
	result := map[string]any{
		"finding_id":  finding.ID,
		"kind":        string(finding.Kind),
		"fingerprint": finding.Fingerprint,
		"score":       finding.Score,
		"members":     members,
		"reasons":     reasons,
	}
	if finding.Confidence > 0 {
		result["confidence"] = finding.Confidence
	}
	if finding.Outlier != nil {
		for key, value := range outlierResultBlock(finding.Outlier) {
			result[key] = value
		}
	}
	return result
}

// outlierResultBlock renders the convention-outlier evidence block: the
// cohort that produced the finding, the majority callee, the share, and
// the outliers. It is the only kind carrying this block.
func outlierResultBlock(detail *OutlierDetail) map[string]any {
	mediated := make([]map[string]any, 0, len(detail.Mediated))
	for _, mediation := range detail.Mediated {
		mediated = append(mediated, map[string]any{
			"outlier_id":    mediation.OutlierID,
			"mediator_id":   mediation.MediatorID,
			"mediator_name": mediation.MediatorName,
		})
	}
	return map[string]any{
		"cohort": map[string]any{
			"source":    string(detail.Source),
			"key":       detail.Key,
			"label":     detail.Label,
			"size":      detail.CohortSize,
			"truncated": detail.Truncated,
			"total":     detail.TotalCohort,
		},
		"majority_callee": map[string]any{
			"entity_id":   detail.CalleeID,
			"entity_name": detail.CalleeName,
		},
		"share":             detail.Share,
		"outliers":          append([]string(nil), detail.OutlierIDs...),
		"inferred":          detail.Inferred,
		"mediated_outliers": mediated,
	}
}
