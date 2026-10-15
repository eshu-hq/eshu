// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import "sort"

// Delta-union completeness report (#7797): per repository, the graph's file
// set against the ACTIVE set (base-full overlaid with the active delta).
// Read-only: the check never repairs, it only names what mismatches so an
// operator can force the full re-parse. Next to delta_active.go, which owns
// the refinalize-time routing for the same scopes.

// DeltaUnionRepoOutcome is one repository's completeness verdict.
type DeltaUnionRepoOutcome string

const (
	// DeltaUnionRepoComplete means the graph holds exactly the ACTIVE set.
	DeltaUnionRepoComplete DeltaUnionRepoOutcome = "complete"
	// DeltaUnionRepoMismatch means the graph differs from the ACTIVE set and
	// names the diff for repair.
	DeltaUnionRepoMismatch DeltaUnionRepoOutcome = "mismatch"
	// DeltaUnionRepoSkipped means the scope carries no active generation, so
	// there is nothing to compare.
	DeltaUnionRepoSkipped DeltaUnionRepoOutcome = "skipped"
)

// DeltaUnionGeneration annotates one generation behind a verdict.
type DeltaUnionGeneration struct {
	// GenerationID names the generation, "" when absent.
	GenerationID string
	// Status is its status (active, completed, or superseded).
	Status string
	// IsDelta tells whether it carries only file-scoped changes.
	IsDelta bool
}

// DeltaUnionRepoReport is one repository's completeness result.
type DeltaUnionRepoReport struct {
	// RepoID and ScopeID identify the repository and its scope.
	RepoID  string `json:"repo_id"`
	ScopeID string `json:"scope_id"`
	// Outcome is the verdict.
	Outcome DeltaUnionRepoOutcome `json:"outcome"`
	// Base and Active annotate the generations behind the verdict.
	Base   DeltaUnionGeneration `json:"base"`
	Active DeltaUnionGeneration `json:"active"`
	// GraphFiles and ActiveFiles count the compared file sets.
	GraphFiles  int `json:"graph_files"`
	ActiveFiles int `json:"active_files"`
	// Missing lists ACTIVE paths absent from the graph, Extra lists graph
	// paths absent from ACTIVE, Drifted lists paths whose entity counts
	// disagree. Empty unless Outcome is mismatch.
	Missing []string `json:"missing,omitempty"`
	Extra   []string `json:"extra,omitempty"`
	Drifted []string `json:"drifted,omitempty"`
	// Annotation names the generations behind the verdict for repair.
	Annotation string `json:"annotation,omitempty"`
}

// DeltaUnionCompletenessReport is the served completeness report: one entry
// per checked repository, sorted by repo id for stable output.
type DeltaUnionCompletenessReport struct {
	// Repos holds one entry per checked repository.
	Repos []DeltaUnionRepoReport `json:"repos"`
	// Mismatches counts mismatch outcomes.
	Mismatches int `json:"mismatches"`
}

// SummarizeDeltaUnionCompleteness folds per-repo results into a report.
func SummarizeDeltaUnionCompleteness(repos []DeltaUnionRepoReport) DeltaUnionCompletenessReport {
	sorted := append([]DeltaUnionRepoReport(nil), repos...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].RepoID != sorted[j].RepoID {
			return sorted[i].RepoID < sorted[j].RepoID
		}
		return sorted[i].ScopeID < sorted[j].ScopeID
	})
	report := DeltaUnionCompletenessReport{Repos: sorted}
	for _, repo := range sorted {
		if repo.Outcome == DeltaUnionRepoMismatch {
			report.Mismatches++
		}
	}
	return report
}
