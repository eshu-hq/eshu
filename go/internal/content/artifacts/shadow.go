// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"slices"
	"strings"
)

// Shadow compare outcomes (#8155 PR3c): the closed alphabet of one related
// repository's stored-vs-live comparison in shadow mode. The set is closed;
// telemetry pins its counter labels to it.
const (
	// ShadowOutcomeMatch is a related repository whose stored rows equal its
	// live rows as multisets.
	ShadowOutcomeMatch = "match"
	// ShadowOutcomeMismatch is a related repository whose stored and live
	// rows differ. The request still serves the live answer.
	ShadowOutcomeMismatch = "mismatch"
	// ShadowOutcomeStoredMissing is a related repository with no trusted
	// stored rows, so there is nothing to compare. It is not a mismatch.
	ShadowOutcomeStoredMissing = "stored_missing"
)

// ShadowOutcomes returns a copy of the closed shadow compare outcome set, in
// declaration order.
func ShadowOutcomes() []string {
	return []string{
		ShadowOutcomeMatch,
		ShadowOutcomeMismatch,
		ShadowOutcomeStoredMissing,
	}
}

// StoredRow is one name-free config_artifact_paths row a stored read returns
// for a related repository: the cleaned path of the build with no repository
// name, the repository-relative file, the extractor's evidence kind, and
// whether the read step strips the repository-name prefix from the path.
type StoredRow struct {
	RawPath      string
	RelativePath string
	EvidenceKind string
	NameScoped   bool
}

// NamedRow is one stored row after the read-time name step, in the row
// identity of configPathKeyOrder (path, source_repo, relative_path,
// evidence_kind). It is a struct, not a row map, so the name-scope source
// guard keeps reading every evidence_kind row literal from the extractors
// alone: no other file in this package builds a row-shaped map.
type NamedRow struct {
	Path         string
	SourceRepo   string
	RelativePath string
	EvidenceKind string
}

// ApplyStoredNameStep runs the read-side name step (decision record section
// 3.3) over stored rows: a name-scoped row's path is the raw path with the
// repository-name prefix stripped (strip only, never a second clean), any
// other row keeps its raw path, and every row's source_repo is the graph
// repository name.
func ApplyStoredNameStep(repoName string, rows []StoredRow) []NamedRow {
	out := make([]NamedRow, 0, len(rows))
	for _, row := range rows {
		path := row.RawPath
		if row.NameScoped {
			path = StripRepositoryName(row.RawPath, repoName)
		}
		out = append(out, NamedRow{
			Path:         path,
			SourceRepo:   repoName,
			RelativePath: row.RelativePath,
			EvidenceKind: row.EvidenceKind,
		})
	}
	return out
}

// CompareNamedRows orders two named rows by the row identity of
// configPathKeyOrder, exactly as CompareConfigPaths orders the equivalent
// row maps.
func CompareNamedRows(left, right NamedRow) int {
	if c := strings.Compare(left.Path, right.Path); c != 0 {
		return c
	}
	if c := strings.Compare(left.SourceRepo, right.SourceRepo); c != 0 {
		return c
	}
	if c := strings.Compare(left.RelativePath, right.RelativePath); c != 0 {
		return c
	}
	return strings.Compare(left.EvidenceKind, right.EvidenceKind)
}

// EqualNamedRows reports whether two per-repository named-row sets hold the
// same multiset. Order does not matter: both sets are sorted before the
// element-wise comparison, so input order never decides a shadow mismatch.
func EqualNamedRows(first, second []NamedRow) bool {
	if len(first) != len(second) {
		return false
	}
	orderedFirst := slices.Clone(first)
	orderedSecond := slices.Clone(second)
	slices.SortFunc(orderedFirst, CompareNamedRows)
	slices.SortFunc(orderedSecond, CompareNamedRows)
	for i := range orderedFirst {
		if orderedFirst[i] != orderedSecond[i] {
			return false
		}
	}
	return true
}
