// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

const documentationStoryReadLimit = 10

func loadServiceStoryTargetDocumentationForOperation(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
	operation string,
) (map[string]any, error) {
	if strings.TrimSpace(operation) != "service_story" {
		return nil, nil
	}
	return loadServiceStoryTargetDocumentation(ctx, content, workloadContext)
}

func loadServiceStoryTargetDocumentation(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	repoID := safeStr(workloadContext, "repo_id")
	serviceID := safeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return nil, nil
	}
	filter := documentationFindingFilter{
		Repository: repoID,
		Limit:      documentationStoryReadLimit,
	}
	if serviceID != "" {
		filter.TargetKind = "service"
		filter.TargetID = serviceID
		filter.ServiceID = serviceID
	} else {
		filter.TargetKind = "repository"
		filter.TargetID = repoID
	}
	return loadStoryTargetDocumentation(ctx, content, filter)
}

func loadRepositoryStoryTargetDocumentation(
	ctx context.Context,
	content ContentStore,
	repoID string,
) (map[string]any, error) {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil, nil
	}
	return loadStoryTargetDocumentation(ctx, content, documentationFindingFilter{
		Repository: repoID,
		TargetKind: "repository",
		TargetID:   repoID,
		Limit:      documentationStoryReadLimit,
	})
}

func loadStoryTargetDocumentation(
	ctx context.Context,
	content ContentStore,
	filter documentationFindingFilter,
) (map[string]any, error) {
	store, ok := content.(documentationReadModelStore)
	if !ok || store == nil {
		return nil, nil
	}
	readModel, err := store.documentationFindings(ctx, filter)
	if err != nil {
		return nil, err
	}
	return buildStoryTargetDocumentation(filter, readModel), nil
}

func attachStoryTargetDocumentation(
	overview map[string]any,
	targetDocumentation map[string]any,
) map[string]any {
	if len(targetDocumentation) == 0 {
		return overview
	}
	if overview == nil {
		overview = map[string]any{}
	}
	overview["target_documentation"] = targetDocumentation
	if story := storyTargetDocumentationSummary(targetDocumentation); story != "" {
		appendStoryField(overview, story)
	}
	return overview
}

func buildStoryTargetDocumentation(
	filter documentationFindingFilter,
	readModel documentationFindingListReadModel,
) map[string]any {
	findings := storyTargetDocumentationFindings(filter, readModel.Findings)
	relatedFacts := readModel.RelatedFacts
	missingEvidence := readModel.MissingEvidence
	if len(missingEvidence) == 0 && readModel.Coverage.Target.hasSelector() {
		missingEvidence = documentationMissingEvidenceForTarget(readModel.Coverage)
	}
	if len(findings) == 0 && len(relatedFacts) == 0 &&
		readModel.Coverage.TargetFactCount == 0 &&
		readModel.Coverage.SourceOnlyCount == 0 {
		return nil
	}
	if findings == nil {
		findings = []map[string]any{}
	}
	if relatedFacts == nil {
		relatedFacts = []map[string]any{}
	}
	return map[string]any{
		"findings":           findings,
		"finding_count":      len(findings),
		"related_facts":      relatedFacts,
		"related_fact_count": len(relatedFacts),
		"coverage":           documentationTargetCoverageMap(readModel.Coverage),
		"missing_evidence":   documentationMissingEvidenceMaps(missingEvidence),
		"limit":              documentationStoryReadLimit,
		"source":             "documentation_read_model",
	}
}

func storyTargetDocumentationFindings(
	filter documentationFindingFilter,
	findings []map[string]any,
) []map[string]any {
	if len(findings) == 0 || !documentationFindingFilterHasExplicitTarget(filter) {
		return findings
	}
	refs := documentationTargetRefsFromFindingFilter(filter)
	if len(refs) == 0 {
		return findings
	}
	filtered := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		if documentationPayloadMatchesTargetRefs(finding, refs) {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

func documentationTargetCoverageMap(coverage documentationTargetCoverage) map[string]any {
	out := map[string]any{
		"findings_returned": coverage.FindingsReturned,
		"target_fact_count": coverage.TargetFactCount,
		"truncated":         coverage.Truncated,
	}
	if coverage.Target.hasSelector() {
		out["target"] = map[string]any{
			"repository":  coverage.Target.Repository,
			"target_kind": coverage.Target.TargetKind,
			"target_id":   coverage.Target.TargetID,
			"service_id":  coverage.Target.ServiceID,
		}
	}
	if len(coverage.TargetFactKinds) > 0 {
		out["target_fact_kinds"] = coverage.TargetFactKinds
	}
	if coverage.SourceOnlyCount > 0 {
		out["source_only_count"] = coverage.SourceOnlyCount
	}
	if len(coverage.SourceOnlyFactKinds) > 0 {
		out["source_only_fact_kinds"] = coverage.SourceOnlyFactKinds
	}
	return out
}

func documentationMissingEvidenceMaps(values []documentationMissingEvidence) []map[string]any {
	if len(values) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row := map[string]any{"reason": value.Reason}
		if strings.TrimSpace(value.Detail) != "" {
			row["detail"] = value.Detail
		}
		out = append(out, row)
	}
	return out
}

func storyTargetDocumentationSummary(targetDocumentation map[string]any) string {
	findingCount := IntVal(targetDocumentation, "finding_count")
	relatedFactCount := IntVal(targetDocumentation, "related_fact_count")
	switch {
	case findingCount > 0:
		return fmt.Sprintf("External documentation includes %d target-linked finding(s).", findingCount)
	case relatedFactCount > 0:
		return fmt.Sprintf("External documentation has %d target-related fact(s) but no admitted finding for this target.", relatedFactCount)
	case IntVal(mapValue(targetDocumentation, "coverage"), "source_only_count") > 0:
		return "External documentation facts exist, but none carry structured refs for this target."
	default:
		return ""
	}
}

func appendStoryField(value map[string]any, fragment string) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return
	}
	story := strings.TrimSpace(StringVal(value, "story"))
	if story == "" {
		value["story"] = fragment
		return
	}
	if strings.Contains(story, fragment) {
		return
	}
	value["story"] = story + " " + fragment
}
