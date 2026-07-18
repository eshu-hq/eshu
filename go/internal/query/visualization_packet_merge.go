// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

func normalizeVisualizationNode(node VisualizationNode) VisualizationNode {
	node.Roles = mergeVisualizationRoles(node.Roles, []string{node.Role})
	node.Role = primaryVisualizationRole(node.Roles)
	if category := visualizationCategoryForRole(node.Role); category != "" {
		node.Category = category
	}
	node.ScopeKeys = mergeVisualizationStrings(node.ScopeKeys, []string{node.ScopeKey})
	node.ScopeKey = firstVisualizationString(node.ScopeKeys)
	return node
}

func mergeVisualizationNodePresentation(left, right VisualizationNode) VisualizationNode {
	leftPriority := visualizationRolePriority(left.Role)
	rightPriority := visualizationRolePriority(right.Role)
	if rightPriority > leftPriority ||
		(rightPriority == leftPriority && visualizationNodePresentationKey(right) < visualizationNodePresentationKey(left)) {
		left.Label = right.Label
		left.Type = right.Type
		left.Category = right.Category
		left.Role = right.Role
		left.CanonicalKey = firstNonEmptyString(right.CanonicalKey, left.CanonicalKey)
	}
	return left
}

func visualizationNodePresentationKey(node VisualizationNode) string {
	return strings.Join([]string{node.Type, node.Category, node.Role, node.Label, node.ScopeKey, node.CanonicalKey}, "\x00")
}

func mergeVisualizationRoles(left, right []string) []string {
	roles := mergeVisualizationStrings(left, right)
	sort.Slice(roles, func(i, j int) bool {
		leftPriority := visualizationRolePriority(roles[i])
		rightPriority := visualizationRolePriority(roles[j])
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return roles[i] < roles[j]
	})
	return roles
}

func visualizationRolePriority(role string) int {
	switch role {
	case "workload":
		return 5
	case "source_repository":
		return 4
	case "runtime_instance":
		return 3
	case "deployment_configuration":
		return 2
	case "downstream_consumer":
		return 1
	default:
		return 0
	}
}

func primaryVisualizationRole(roles []string) string {
	return firstVisualizationString(roles)
}

func visualizationCategoryForRole(role string) string {
	switch role {
	case "workload":
		return "service"
	case "source_repository":
		return "source"
	case "runtime_instance":
		return "runtime"
	case "deployment_configuration":
		return "deployment"
	case "downstream_consumer":
		return "downstream"
	default:
		return ""
	}
}

func mergeVisualizationStrings(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for _, value := range append(append([]string{}, left...), right...) {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func firstVisualizationString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func mergeVisualizationEvidenceHandles(
	left []evidenceCitationHandle,
	right []evidenceCitationHandle,
	extra ...*evidenceCitationHandle,
) []evidenceCitationHandle {
	merged := make(map[evidenceCitationHandleKey]evidenceCitationHandle, len(left)+len(right)+len(extra))
	for _, handle := range append(append([]evidenceCitationHandle{}, left...), right...) {
		merged[handle.evidenceCitationHandleKey()] = handle
	}
	for _, handle := range extra {
		if handle != nil {
			merged[handle.evidenceCitationHandleKey()] = *handle
		}
	}
	result := make([]evidenceCitationHandle, 0, len(merged))
	for _, handle := range merged {
		result = append(result, handle)
	}
	sort.Slice(result, func(i, j int) bool {
		return visualizationEvidenceHandleSortKey(result[i]) < visualizationEvidenceHandleSortKey(result[j])
	})
	return result
}

func visualizationEvidenceHandleSortKey(handle evidenceCitationHandle) string {
	return strings.Join([]string{
		handle.Kind,
		handle.RepoID,
		handle.RelativePath,
		handle.EntityID,
		handle.EvidenceFamily,
		handle.Reason,
		fmt.Sprint(handle.StartLine),
		fmt.Sprint(handle.EndLine),
	}, "\x00")
}

func strongerVisualizationTruthLabel(left, right string) string {
	leftRank := visualizationTruthLabelRank(left)
	rightRank := visualizationTruthLabelRank(right)
	if rightRank > leftRank || (rightRank == leftRank && right != "" && (left == "" || right < left)) {
		return right
	}
	return left
}

func visualizationTruthLabelRank(label string) int {
	switch label {
	case string(TruthLevelExact):
		return 3
	case string(TruthLevelDerived):
		return 2
	case string(TruthLevelFallback):
		return 1
	default:
		return 0
	}
}
