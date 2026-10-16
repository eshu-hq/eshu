// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"path"
	"sort"
	"strings"
)

// File-attributable fact kinds carry one file's rows under a top-level path
// payload key. The delta union drops a base row of these kinds when its file
// is one of the delta's touched (changed or deleted) paths, so a deleted
// file is not resurrected and a changed file's stale rows do not survive
// beside the delta's complete fresh set. Every other kind overlays by stable
// key only: a delta row replaces the same-key base row (a tombstone removes
// it) and untouched base rows are kept.
const (
	// deltaUnionRelativePathKey is the top-level payload key naming a fact's
	// file for the file, content_entity, and parsed_file_data kinds, matching
	// the codegraph file and entity extractors.
	deltaUnionRelativePathKey = "relative_path"
	// deltaUnionContentPathKey is the top-level payload key naming a fact's
	// file for the content kind.
	deltaUnionContentPathKey = "content_path"
)

// deltaUnionFileKinds are the fact kinds attributed to one file by payload
// path for touched-path removal.
var deltaUnionFileKinds = map[string]string{
	"file":             deltaUnionRelativePathKey,
	"content_entity":   deltaUnionRelativePathKey,
	"parsed_file_data": deltaUnionRelativePathKey,
	"content":          deltaUnionContentPathKey,
}

// DeltaScope is one delta generation's file-scoped change set, parsed from
// its repository fact payload. Changed and Deleted hold repo-qualified paths;
// out-of-repo entries are dropped at parse time, never matched later.
type DeltaScope struct {
	// IsDelta reports whether the generation carries delta_generation facts.
	IsDelta bool
	// RepoPath is the repository root the paths are qualified against,
	// from the delta repository fact's path (or local_path fallback).
	RepoPath string
	// Changed holds the qualified delta_relative_paths.
	Changed []string
	// Deleted holds the qualified delta_deleted_relative_paths.
	Deleted []string
}

// ParseDeltaScope reads the delta scope from delta envelopes' repository
// fact: delta_generation marks the generation, delta_relative_paths names
// changed files, delta_deleted_relative_paths names deleted files, both
// relative to the repository root. It mirrors
// projector/canonical's qualifyDeltaRelativePaths contract (clean, reject
// parent escapes and absolute paths, dedupe) so the union and the projector
// agree on which paths a delta touches.
func ParseDeltaScope(delta []Envelope) DeltaScope {
	var scope DeltaScope
	for i := range delta {
		if normalizeDeltaKind(delta[i].FactKind) != "repository" {
			continue
		}
		payload := delta[i].Payload
		if !deltaPayloadBool(payload, "delta_generation") {
			continue
		}
		scope.IsDelta = true
		scope.RepoPath = deltaRepoPath(payload)
		scope.Changed = qualifyDeltaUnionPaths(scope.RepoPath, deltaPayloadStrings(payload, "delta_relative_paths"))
		scope.Deleted = qualifyDeltaUnionPaths(scope.RepoPath, deltaPayloadStrings(payload, "delta_deleted_relative_paths"))
		return scope
	}
	return scope
}

// Touched returns the changed and deleted paths as one deduped set.
func (s DeltaScope) Touched() map[string]struct{} {
	touched := make(map[string]struct{}, len(s.Changed)+len(s.Deleted))
	for _, p := range s.Changed {
		touched[p] = struct{}{}
	}
	for _, p := range s.Deleted {
		touched[p] = struct{}{}
	}
	return touched
}

// OverlayDeltaBase merges one delta generation's facts over its base full
// generation's facts so a from-empty projection of the delta writes the full
// repository tree, not only the delta's files:
//
//   - every delta row is kept, including tombstones and the delta repository
//     fact (which keeps delta_generation, so downstream retracts stay
//     file-scoped while writes carry the full set);
//   - a base row whose non-empty stable key matches any delta row's key is
//     dropped: the delta wins, and a delta tombstone removes the base row;
//   - a base row of a file-attributable kind whose file is one of the
//     delta's touched paths is dropped: the delta carries the complete fresh
//     set for changed files, and deleted files carry no rows by definition;
//   - every other base row is kept.
//
// Base rows keep their own generation identity; callers must not rewrite it.
// The merged set is ordered by (observed_at, fact_id), matching the ListFacts
// contract. When scope is not a delta generation nothing is dropped: the
// output is the stable-ordered concatenation.
func OverlayDeltaBase(base, delta []Envelope, scope DeltaScope) []Envelope {
	if !scope.IsDelta {
		return stableOrderEnvelopes(base, delta)
	}
	deltaKeys := make(map[string]struct{}, len(delta))
	for i := range delta {
		if key := strings.TrimSpace(delta[i].StableFactKey); key != "" {
			deltaKeys[key] = struct{}{}
		}
	}
	touched := scope.Touched()
	merged := make([]Envelope, 0, len(base)+len(delta))
	for i := range base {
		if key := strings.TrimSpace(base[i].StableFactKey); key != "" {
			if _, replaced := deltaKeys[key]; replaced {
				continue
			}
		}
		if _, touched := touched[deltaUnionFilePath(base[i], scope.RepoPath)]; touched {
			continue
		}
		merged = append(merged, base[i])
	}
	merged = append(merged, delta...)
	return stableOrderEnvelopes(merged, nil)
}

// deltaUnionFilePath returns the repo-qualified file path of a
// file-attributable envelope, or "" when the kind is not attributable or the
// payload carries no usable path.
func deltaUnionFilePath(env Envelope, repoPath string) string {
	pathKey, ok := deltaUnionFileKinds[normalizeDeltaKind(env.FactKind)]
	if !ok {
		return ""
	}
	rel, ok := env.Payload[pathKey].(string)
	if !ok {
		return ""
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	return qualifyDeltaUnionPath(repoPath, rel)
}

// stableOrderEnvelopes concatenates the inputs in stable (observed_at,
// fact_id) order, matching the ListFacts read contract.
func stableOrderEnvelopes(groups ...[]Envelope) []Envelope {
	var out []Envelope
	for _, group := range groups {
		out = append(out, group...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ObservedAt.Equal(out[j].ObservedAt) {
			return out[i].FactID < out[j].FactID
		}
		return out[i].ObservedAt.Before(out[j].ObservedAt)
	})
	return out
}

// normalizeDeltaKind strips the legacy "Fact" suffix, matching
// projector/decode's NormalizeFactKind without importing it.
func normalizeDeltaKind(kind string) string {
	return strings.TrimSuffix(kind, "Fact")
}

// deltaRepoPath reads the repository root from a repository fact payload:
// path first, local_path fallback, matching the canonical repository
// extractor's fallback.
func deltaRepoPath(payload map[string]any) string {
	if p, ok := payload["path"].(string); ok && strings.TrimSpace(p) != "" {
		return strings.TrimSpace(p)
	}
	if p, ok := payload["local_path"].(string); ok {
		return strings.TrimSpace(p)
	}
	return ""
}

// qualifyDeltaUnionPaths qualifies relative paths against repoPath, dropping
// out-of-repo entries (parent escapes, absolute paths, empties) and
// duplicates. It mirrors projector/canonical's qualifyDeltaRelativePaths.
func qualifyDeltaUnionPaths(repoPath string, relativePaths []string) []string {
	qualified := make([]string, 0, len(relativePaths))
	seen := make(map[string]struct{}, len(relativePaths))
	for _, relativePath := range relativePaths {
		fullPath := qualifyDeltaUnionPath(repoPath, relativePath)
		if fullPath == "" {
			continue
		}
		if _, ok := seen[fullPath]; ok {
			continue
		}
		seen[fullPath] = struct{}{}
		qualified = append(qualified, fullPath)
	}
	return qualified
}

// qualifyDeltaUnionPath qualifies one relative path, returning "" for an
// out-of-repo entry.
func qualifyDeltaUnionPath(repoPath, relativePath string) string {
	cleaned := path.Clean(strings.TrimSpace(relativePath))
	if cleaned == "." || cleaned == "" || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return ""
	}
	if repoPath == "" {
		return cleaned
	}
	return repoPath + "/" + cleaned
}

// deltaPayloadBool reads a top-level boolean payload flag.
func deltaPayloadBool(payload map[string]any, key string) bool {
	value, ok := payload[key].(bool)
	return ok && value
}

// deltaPayloadStrings reads a top-level string-slice payload value,
// accepting the []string and []any forms the collector emits.
func deltaPayloadStrings(payload map[string]any, key string) []string {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		var out []string
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		var out []string
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
