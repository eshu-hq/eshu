// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"path"

	"github.com/eshu-hq/eshu/go/internal/projector/decode"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func extractDeltaProjectionScope(envelopes []facts.Envelope, repoPath string) (bool, []string, []string) {
	repoFacts := decode.FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return false, nil, nil
	}
	delta := decode.PayloadBoolPtr(repoFacts[0].Payload, "delta_generation")
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
	repoFacts := decode.FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return false
	}
	reconcile := decode.PayloadBoolPtr(repoFacts[0].Payload, "reconciliation_generation")
	return reconcile != nil && *reconcile
}

// extractDeclaredEmptyScope reports whether the generation's repository fact
// declares `empty_scope`: the collector observed zero files, so a file-less
// materialization is genuine filelessness rather than facts gone missing
// downstream (issue #8668). It follows the same in-band declaration pattern
// as the delta and reconciliation keys above: a raw optional bool on the
// repository fact, read off the handed envelopes with no store access, so
// replay of the same facts derives the same value. Only an explicit true
// declares -- through decode.PayloadBoolPtr, so a parseable-string "true"
// declares exactly as it would for the delta keys; an absent key, an
// explicit false, or an unparseable value declares nothing.
// The declaration excuses only zero-file-fact builds at
// the writer-side retract check (#8657): live file facts that drop still
// fail loud through rejectSilentFilelessWipe, marker or not.
func extractDeclaredEmptyScope(envelopes []facts.Envelope) bool {
	repoFacts := decode.FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return false
	}
	declared := decode.PayloadBoolPtr(repoFacts[0].Payload, "empty_scope")
	return declared != nil && *declared
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
