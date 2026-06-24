// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"path"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func extractDeltaProjectionScope(envelopes []facts.Envelope, repoPath string) (bool, []string, []string) {
	repoFacts := FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return false, nil, nil
	}
	delta := payloadBoolPtr(repoFacts[0].Payload, "delta_generation")
	if delta == nil || !*delta {
		return false, nil, nil
	}
	paths := qualifyDeltaRelativePaths(repoPath, deltaPayloadStringSlice(repoFacts[0].Payload, "delta_relative_paths"))
	deletedPaths := qualifyDeltaRelativePaths(repoPath, deltaPayloadStringSlice(repoFacts[0].Payload, "delta_deleted_relative_paths"))
	if len(paths) == 0 && len(deletedPaths) == 0 {
		return false, nil, nil
	}
	paths = appendMissingProjectorStrings(paths, deletedPaths)
	return true, paths, deletedPaths
}

func extractReconciliationProjection(envelopes []facts.Envelope) bool {
	repoFacts := FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return false
	}
	reconcile := payloadBoolPtr(repoFacts[0].Payload, "reconciliation_generation")
	return reconcile != nil && *reconcile
}

func qualifyDeltaRelativePaths(repoPath string, relativePaths []string) []string {
	if len(relativePaths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(relativePaths))
	qualified := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		cleaned := path.Clean(relativePath)
		if cleaned == "." || cleaned == "" || cleaned == ".." ||
			pathHasParentPrefix(cleaned) || path.IsAbs(cleaned) {
			continue
		}
		fullPath := qualifyPath(repoPath, cleaned)
		if _, ok := seen[fullPath]; ok {
			continue
		}
		seen[fullPath] = struct{}{}
		qualified = append(qualified, fullPath)
	}
	return qualified
}

func deltaPayloadStringSlice(payload map[string]any, key string) []string {
	if len(payload) == 0 {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok || text == "" {
				continue
			}
			out = append(out, text)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func pathHasParentPrefix(value string) bool {
	return len(value) >= 3 && value[:3] == "../"
}

func appendMissingProjectorStrings(values []string, more []string) []string {
	if len(more) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values)+len(more))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range more {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
