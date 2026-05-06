package query

import (
	"slices"
	"strings"
)

func buildRepositoryRelationshipOverview(relationships []map[string]any) map[string]any {
	rows := make([]map[string]any, 0, len(relationships))
	for _, relationship := range relationships {
		direction := StringVal(relationship, "direction")
		relType := StringVal(relationship, "type")
		sourceName := StringVal(relationship, "source_name")
		sourceID := StringVal(relationship, "source_id")
		targetName := StringVal(relationship, "target_name")
		targetID := StringVal(relationship, "target_id")
		evidenceType := StringVal(relationship, "evidence_type")
		if relType == "" && sourceName == "" && sourceID == "" && targetName == "" && targetID == "" && evidenceType == "" {
			continue
		}
		row := map[string]any{
			"type":        relType,
			"target_name": targetName,
			"target_id":   targetID,
		}
		if direction != "" {
			row["direction"] = direction
		}
		if sourceName != "" {
			row["source_name"] = sourceName
		}
		if sourceID != "" {
			row["source_id"] = sourceID
		}
		if evidenceType != "" {
			row["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(row, relationship)
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil
	}

	slices.SortFunc(rows, func(left, right map[string]any) int {
		if cmp := strings.Compare(StringVal(left, "direction"), StringVal(right, "direction")); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(StringVal(left, "type"), StringVal(right, "type")); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(StringVal(left, "source_name"), StringVal(right, "source_name")); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(StringVal(left, "target_name"), StringVal(right, "target_name")); cmp != 0 {
			return cmp
		}
		return strings.Compare(StringVal(left, "evidence_type"), StringVal(right, "evidence_type"))
	})

	controllerDriven := filterRepositoryRelationshipsByEvidencePrefix(rows, controllerEvidenceTypePrefixes...)
	workflowDriven := filterRepositoryRelationshipsByEvidence(rows, "github_actions_")
	iacDriven := filterRepositoryRelationshipsByEvidencePrefix(rows, iacEvidenceTypePrefixes...)
	otherTyped := excludeRepositoryRelationships(rows, controllerDriven, workflowDriven, iacDriven)

	overview := map[string]any{
		"relationship_count": len(rows),
		"relationships":      rows,
		"relationship_types": uniqueRelationshipStrings(rows, "type"),
		"evidence_types":     uniqueRelationshipStrings(rows, "evidence_type"),
	}
	if len(controllerDriven) > 0 {
		overview["controller_driven"] = controllerDriven
	}
	if len(workflowDriven) > 0 {
		overview["workflow_driven"] = workflowDriven
	}
	if len(iacDriven) > 0 {
		overview["iac_driven"] = iacDriven
	}
	if len(otherTyped) > 0 {
		overview["other_relationships"] = otherTyped
	}
	if story := buildRepositoryRelationshipStory(overview); story != "" {
		overview["story"] = story
	}

	return overview
}

// copyRelationshipEvidenceMetadata keeps graph-edge evidence pointers visible
// on query responses without embedding full Postgres evidence details.
func copyRelationshipEvidenceMetadata(dst map[string]any, src map[string]any) {
	if resolvedID := StringVal(src, "resolved_id"); resolvedID != "" {
		dst["resolved_id"] = resolvedID
	}
	if generationID := StringVal(src, "generation_id"); generationID != "" {
		dst["generation_id"] = generationID
	}
	if confidence := relationshipFloatVal(src, "confidence"); confidence > 0 {
		dst["confidence"] = confidence
	}
	if evidenceCount := IntVal(src, "evidence_count"); evidenceCount > 0 {
		dst["evidence_count"] = evidenceCount
	}
	if evidenceKinds := StringSliceVal(src, "evidence_kinds"); len(evidenceKinds) > 0 {
		dst["evidence_kinds"] = evidenceKinds
	}
	if resolutionSource := StringVal(src, "resolution_source"); resolutionSource != "" {
		dst["resolution_source"] = resolutionSource
	}
	if rationale := StringVal(src, "rationale"); rationale != "" {
		dst["rationale"] = rationale
	}
}

func buildRepositoryRelationshipStory(overview map[string]any) string {
	if len(overview) == 0 {
		return ""
	}

	parts := make([]string, 0, 3)
	if controllerDriven := mapSliceValue(overview, "controller_driven"); len(controllerDriven) > 0 {
		parts = append(parts, "Controller-driven relationships: "+joinSentenceFragments(relationshipSummaries(controllerDriven))+".")
	}
	if workflowDriven := mapSliceValue(overview, "workflow_driven"); len(workflowDriven) > 0 {
		parts = append(parts, "Workflow-driven relationships: "+joinSentenceFragments(relationshipSummaries(workflowDriven))+".")
	}
	if iacDriven := mapSliceValue(overview, "iac_driven"); len(iacDriven) > 0 {
		parts = append(parts, "IaC-driven relationships: "+joinSentenceFragments(relationshipSummaries(iacDriven))+".")
	}
	if otherRelationships := mapSliceValue(overview, "other_relationships"); len(otherRelationships) > 0 {
		parts = append(parts, "Other typed relationships: "+joinSentenceFragments(relationshipSummaries(otherRelationships))+".")
	}
	return strings.Join(parts, " ")
}

func filterRepositoryRelationshipsByEvidence(rows []map[string]any, prefix string) []map[string]any {
	return filterRepositoryRelationshipsByEvidencePrefix(rows, prefix)
}

func filterRepositoryRelationshipsByEvidencePrefix(rows []map[string]any, prefixes ...string) []map[string]any {
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		evidenceType := StringVal(row, "evidence_type")
		for _, prefix := range prefixes {
			if strings.HasPrefix(evidenceType, prefix) {
				filtered = append(filtered, row)
				break
			}
		}
	}
	return filtered
}

func excludeRepositoryRelationships(rows []map[string]any, groups ...[]map[string]any) []map[string]any {
	if len(rows) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(rows))
	for _, group := range groups {
		for _, row := range group {
			seen[repositoryRelationshipKey(row)] = struct{}{}
		}
	}

	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[repositoryRelationshipKey(row)]; ok {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func repositoryRelationshipKey(row map[string]any) string {
	return strings.Join([]string{
		StringVal(row, "direction"),
		StringVal(row, "type"),
		StringVal(row, "source_name"),
		StringVal(row, "source_id"),
		StringVal(row, "target_name"),
		StringVal(row, "target_id"),
		StringVal(row, "evidence_type"),
	}, "|")
}

func relationshipSummaries(rows []map[string]any) []string {
	summaries := make([]string, 0, len(rows))
	for _, row := range rows {
		relType := StringVal(row, "type")
		direction := StringVal(row, "direction")
		sourceName := StringVal(row, "source_name")
		targetName := StringVal(row, "target_name")
		evidenceType := StringVal(row, "evidence_type")
		parts := make([]string, 0, 3)
		if direction == "incoming" && sourceName != "" {
			parts = append(parts, sourceName)
		}
		if relType != "" {
			parts = append(parts, relType)
		}
		if direction != "incoming" && targetName != "" {
			parts = append(parts, targetName)
		}
		if evidenceType != "" {
			parts = append(parts, "via "+evidenceType)
		}
		summaries = append(summaries, strings.Join(parts, " "))
	}
	return summaries
}

func uniqueRelationshipStrings(rows []map[string]any, key string) []string {
	values := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		value := strings.TrimSpace(StringVal(row, key))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	slices.Sort(values)
	return values
}

func relationshipFloatVal(row map[string]any, key string) float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

var iacEvidenceTypePrefixes = []string{
	"docker_compose_",
	"dockerfile_",
	"helm_",
	"kustomize_",
	"terraform_",
	"terragrunt_",
}

var controllerEvidenceTypePrefixes = []string{
	"argocd_",
	"ansible_",
	"jenkins_",
}
