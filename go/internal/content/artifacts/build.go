// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// ConfigPathsKey is the key of the row list in the map BuildConfigPaths and the
// merged load return.
const ConfigPathsKey = "config_paths"

// BuildConfigPaths extracts the config-path rows of one repository from its
// hydrated files. It returns nil when no extractor emits a row, and otherwise
// a map with the rows under ConfigPathsKey, each row a map of path,
// source_repo, relative_path and evidence_kind. The rows are in the total
// order of CompareConfigPaths.
//
// repoName enters the output at exactly four places: the source_repo of every
// row, and, for the name-scoped evidence kinds only (NameScoped), the path,
// the per-extractor dedup key and the sort. BuildConfigPaths("", files) is the
// name-free build a stored read model keeps; the read-time name step is
// StripRepositoryName on the name-scoped rows, then source_repo.
func BuildConfigPaths(repoName string, files []File) map[string]any {
	configPaths := make([]map[string]any, 0)
	configPaths = append(configPaths, extractKustomizeConfigPathRows(repoName, files)...)
	configPaths = append(configPaths, extractDockerComposeConfigPathRows(repoName, files)...)
	configPaths = append(configPaths, extractHCLConfigAssetRows(repoName, files)...)
	configPaths = append(configPaths, extractAnsibleConfigPathRows(repoName, files)...)
	if len(configPaths) == 0 {
		return nil
	}
	SortConfigPaths(configPaths)
	return map[string]any{ConfigPathsKey: configPaths}
}

// CompareConfigPaths orders two config-path rows by path, source_repo,
// relative_path and evidence_kind. After the dedup on exactly those four
// fields (MergeConfigPaths, and each extractor's own dedup) no two rows
// compare equal, so the order is total and does not depend on input order.
// SortConfigPaths adds a stable tie-breaker for a caller that sorts rows it
// did not deduplicate.
func CompareConfigPaths(left, right map[string]any) int {
	for _, key := range configPathKeyOrder {
		if c := strings.Compare(stringValue(left, key), stringValue(right, key)); c != 0 {
			return c
		}
	}
	return 0
}

// configPathKeyOrder is the identity of a config-path row, in comparison order.
// It is the one place the four fields and their order are written: it is the
// sort key (CompareConfigPaths), the dedup key of a merged load
// (configPathDedupKey) and the per-extractor dedup key
// (appendConfigArtifactRow), so the three cannot drift.
var configPathKeyOrder = [...]string{"path", "source_repo", "relative_path", "evidence_kind"}

// configPathKeySeparator joins the four fields of a row identity. A field that
// holds the separator can make two different rows share a key; the live path
// has always behaved so, and a stored read model must match it exactly.
const configPathKeySeparator = "|"

// SortConfigPaths sorts rows in place by CompareConfigPaths. The sort is
// stable, so rows that compare equal (none, after the dedup) keep their input
// order: the tie-breaker is the input position.
func SortConfigPaths(rows []map[string]any) {
	slices.SortStableFunc(rows, CompareConfigPaths)
}

// configPathDedupKey is the identity of a row: its four key fields in
// configPathKeyOrder, joined with configPathKeySeparator.
func configPathDedupKey(row map[string]any) string {
	var fields [len(configPathKeyOrder)]string
	for i, field := range configPathKeyOrder {
		fields[i] = stringValue(row, field)
	}
	return strings.Join(fields[:], configPathKeySeparator)
}

// MergeConfigPaths merges the rows of several repositories, in the order the
// repositories are given, with one dedup map across all of them and one total
// sort at the end. The first row with a given (path, source_repo,
// relative_path, evidence_kind) wins. Two repositories that share a graph name,
// or an anchor that also appears in the related list, collapse here exactly as
// they do in the live load. It returns nil when there are no rows.
func MergeConfigPaths(sources ...[]map[string]any) []map[string]any {
	var rows []map[string]any
	seen := map[string]struct{}{}
	for _, source := range sources {
		for _, row := range source {
			key := configPathDedupKey(row)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	SortConfigPaths(rows)
	return rows
}

// ConfigPathRows returns the rows under ConfigPathsKey of a BuildConfigPaths
// result.
func ConfigPathRows(result map[string]any) []map[string]any {
	switch typed := result[ConfigPathsKey].(type) {
	case []map[string]any:
		return typed
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
		return rows
	default:
		return nil
	}
}

// stringValue reads a string column of a row. A missing or nil column is the
// empty string; a non-string value is formatted with %v, as the query row
// helpers do.
func stringValue(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

// isConfigArtifactCandidate reports whether a file can carry a config-artifact
// row: a kustomization, a Docker Compose file, a Terragrunt or Terraform file,
// or a YAML or JSON file. It is the hydration candidate predicate, so the set
// of files read cannot drift from the set the extractors consider.
func isConfigArtifactCandidate(file File) bool {
	relativePath := strings.TrimSpace(file.RelativePath)
	if relativePath == "" {
		return false
	}

	lowerBase := strings.ToLower(path.Base(relativePath))
	switch {
	case lowerBase == "kustomization.yaml", lowerBase == "kustomization.yml", lowerBase == "kustomization":
		return true
	case strings.HasPrefix(lowerBase, "docker-compose"):
		return true
	case lowerBase == "terragrunt.hcl":
		return true
	case strings.HasSuffix(lowerBase, ".tfvars"), strings.HasSuffix(lowerBase, ".tfvars.json"):
		return true
	case strings.HasSuffix(lowerBase, ".tf"), strings.HasSuffix(lowerBase, ".hcl"):
		return true
	case strings.HasSuffix(lowerBase, ".yaml"), strings.HasSuffix(lowerBase, ".yml"), strings.HasSuffix(lowerBase, ".json"):
		return true
	default:
		return false
	}
}

// appendConfigArtifactRow appends one name-scoped row (HCL and Docker Compose
// evidence kinds): the path is cleaned, then the repository name is stripped
// from it, and the row is added once per (path, source_repo, relative_path,
// evidence_kind). Every kind passed here must be true in NameScoped; a guard
// test reads the call sites and the table and fails on a mismatch.
func appendConfigArtifactRow(rows []map[string]any, seen map[string]struct{}, pathValue, repoName, relativePath, evidenceKind string) []map[string]any {
	pathValue = normalizeRepoScopedPath(pathValue, repoName)
	if pathValue == "" {
		return rows
	}
	row := map[string]any{
		"path":          pathValue,
		"source_repo":   repoName,
		"relative_path": relativePath,
		"evidence_kind": evidenceKind,
	}
	key := configPathDedupKey(row)
	if _, ok := seen[key]; ok {
		return rows
	}
	seen[key] = struct{}{}
	return append(rows, row)
}
