// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type semanticDeltaProjectionScope struct {
	Delta     bool
	FilePaths []string
}

func extractSemanticDeltaProjectionScope(
	envelopes []facts.Envelope,
	rows []SemanticEntityRow,
	targetRepoID string,
) semanticDeltaProjectionScope {
	targetRepoID = strings.TrimSpace(targetRepoID)
	seen := make(map[string]struct{})
	scope := semanticDeltaProjectionScope{}

	addFilePath := func(filePath string) {
		if filePath == "" {
			return
		}
		if _, ok := seen[filePath]; ok {
			return
		}
		seen[filePath] = struct{}{}
		scope.FilePaths = append(scope.FilePaths, filePath)
	}

	for _, env := range envelopes {
		if env.FactKind != factKindRepository {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		if targetRepoID != "" && repoID != targetRepoID {
			continue
		}
		if !semanticDeltaPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		scope.Delta = true
		repoPath := semanticDeltaPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = semanticDeltaPayloadString(env.Payload, "local_path")
		}
		for _, relativePath := range semanticDeltaPayloadStringSlice(env.Payload, "delta_relative_paths") {
			addFilePath(semanticQualifyDeltaPath(repoPath, relativePath))
		}
		for _, relativePath := range semanticDeltaPayloadStringSlice(env.Payload, "delta_deleted_relative_paths") {
			addFilePath(semanticQualifyDeltaPath(repoPath, relativePath))
		}
	}
	if !scope.Delta {
		return semanticDeltaProjectionScope{}
	}

	for _, row := range rows {
		if targetRepoID != "" && row.RepoID != targetRepoID {
			continue
		}
		addFilePath(row.FilePath)
	}
	return scope
}

func semanticQualifyDeltaPath(repoPath string, relativePath string) string {
	if repoPath == "" || relativePath == "" {
		return ""
	}
	cleaned := path.Clean(relativePath)
	if cleaned == "" || cleaned == "." || cleaned == ".." ||
		path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return path.Join(repoPath, cleaned)
}

func semanticDeltaPayloadBool(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func semanticDeltaPayloadString(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func semanticDeltaPayloadStringSlice(payload map[string]any, key string) []string {
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
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
