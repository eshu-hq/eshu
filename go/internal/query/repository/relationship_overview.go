// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func BuildRepositoryRelationshipOverview(relationships []map[string]any) map[string]any {
	rows := make([]map[string]any, 0, len(relationships))
	for _, relationship := range relationships {
		direction := querycontract.StringVal(relationship, "direction")
		relType := querycontract.StringVal(relationship, "type")
		sourceName := querycontract.StringVal(relationship, "source_name")
		sourceID := querycontract.StringVal(relationship, "source_id")
		targetName := querycontract.StringVal(relationship, "target_name")
		targetID := querycontract.StringVal(relationship, "target_id")
		evidenceType := querycontract.StringVal(relationship, "evidence_type")
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
		if cmp := strings.Compare(querycontract.StringVal(left, "direction"), querycontract.StringVal(right, "direction")); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(querycontract.StringVal(left, "type"), querycontract.StringVal(right, "type")); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(querycontract.StringVal(left, "source_name"), querycontract.StringVal(right, "source_name")); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(querycontract.StringVal(left, "target_name"), querycontract.StringVal(right, "target_name")); cmp != 0 {
			return cmp
		}
		return strings.Compare(querycontract.StringVal(left, "evidence_type"), querycontract.StringVal(right, "evidence_type"))
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
	if resolvedID := querycontract.StringVal(src, "resolved_id"); resolvedID != "" {
		dst["resolved_id"] = resolvedID
	}
	if generationID := querycontract.StringVal(src, "generation_id"); generationID != "" {
		dst["generation_id"] = generationID
	}
	if confidence := querycontract.FloatVal(src, "confidence"); confidence > 0 {
		dst["confidence"] = confidence
	}
	if evidenceCount := querycontract.IntVal(src, "evidence_count"); evidenceCount > 0 {
		dst["evidence_count"] = evidenceCount
	}
	if evidenceKinds := querycontract.StringSliceVal(src, "evidence_kinds"); len(evidenceKinds) > 0 {
		dst["evidence_kinds"] = evidenceKinds
	}
	if resolutionSource := querycontract.StringVal(src, "resolution_source"); resolutionSource != "" {
		dst["resolution_source"] = resolutionSource
	}
	if confidenceBasis := querycontract.StringVal(src, "confidence_basis"); confidenceBasis != "" {
		dst["confidence_basis"] = confidenceBasis
	}
	if rationale := querycontract.StringVal(src, "rationale"); rationale != "" {
		dst["rationale"] = rationale
	}
	querycontract.AddRelationshipConfidenceBasis(dst)
}

func buildRepositoryRelationshipStory(overview map[string]any) string {
	if len(overview) == 0 {
		return ""
	}

	parts := make([]string, 0, 3)
	if controllerDriven := querycontract.MapSliceValue(overview, "controller_driven"); len(controllerDriven) > 0 {
		parts = append(parts, "Controller-driven relationships: "+querycontract.JoinSentenceFragments(relationshipSummaries(controllerDriven))+".")
	}
	if workflowDriven := querycontract.MapSliceValue(overview, "workflow_driven"); len(workflowDriven) > 0 {
		parts = append(parts, "Workflow-driven relationships: "+querycontract.JoinSentenceFragments(relationshipSummaries(workflowDriven))+".")
	}
	if iacDriven := querycontract.MapSliceValue(overview, "iac_driven"); len(iacDriven) > 0 {
		parts = append(parts, "IaC-driven relationships: "+querycontract.JoinSentenceFragments(relationshipSummaries(iacDriven))+".")
	}
	if otherRelationships := querycontract.MapSliceValue(overview, "other_relationships"); len(otherRelationships) > 0 {
		parts = append(parts, "Other typed relationships: "+querycontract.JoinSentenceFragments(relationshipSummaries(otherRelationships))+".")
	}
	return strings.Join(parts, " ")
}

func filterRepositoryRelationshipsByEvidence(rows []map[string]any, prefix string) []map[string]any {
	return filterRepositoryRelationshipsByEvidencePrefix(rows, prefix)
}

func filterRepositoryRelationshipsByEvidencePrefix(rows []map[string]any, prefixes ...string) []map[string]any {
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		evidenceType := querycontract.StringVal(row, "evidence_type")
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
		querycontract.StringVal(row, "direction"),
		querycontract.StringVal(row, "type"),
		querycontract.StringVal(row, "source_name"),
		querycontract.StringVal(row, "source_id"),
		querycontract.StringVal(row, "target_name"),
		querycontract.StringVal(row, "target_id"),
		querycontract.StringVal(row, "evidence_type"),
	}, "|")
}

func relationshipSummaries(rows []map[string]any) []string {
	summaries := make([]string, 0, len(rows))
	for _, row := range rows {
		relType := querycontract.StringVal(row, "type")
		direction := querycontract.StringVal(row, "direction")
		sourceName := querycontract.StringVal(row, "source_name")
		targetName := querycontract.StringVal(row, "target_name")
		evidenceType := querycontract.StringVal(row, "evidence_type")
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
		value := strings.TrimSpace(querycontract.StringVal(row, key))
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

// relationshipFloatVal forwards to querycontract.FloatVal. The coercion lives
// in the contract leaf beside StringVal/IntVal/StringSliceVal so a handler
// family reads a driver row exactly the way root does; a package-local copy
// would drift the moment a driver returns a new numeric type.

// buildRepositoryRelationshipOverview keeps the in-package spelling after the
// #6060 export; root stayers name BuildRepositoryRelationshipOverview.
func buildRepositoryRelationshipOverview(relationships []map[string]any) map[string]any {
	return BuildRepositoryRelationshipOverview(relationships)
}
