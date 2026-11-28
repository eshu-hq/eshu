// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"fmt"

	apilibadmin "github.com/eshu-hq/eshu/go/apilib/admin"
)

// The delta-union expectation assembly and comparison live in apilib so the
// external API host consumes the same implementation (#11697). Core keeps
// only aliases plus the gate-report wrapper below; there is deliberately
// no second copy of the assembly logic to drift from apilib.

// DeltaUnionExpectation is one repository's ACTIVE side (see apilib).
type DeltaUnionExpectation = apilibadmin.DeltaUnionExpectation

// DeltaUnionObservation is one repository's GRAPH side (see apilib).
type DeltaUnionObservation = apilibadmin.DeltaUnionObservation

// DeltaUnionActiveRow is one fact row behind the ACTIVE side (see apilib).
type DeltaUnionActiveRow = apilibadmin.DeltaUnionActiveRow

// DeltaUnionDeltaPaths carries one active delta's touched paths (see apilib).
type DeltaUnionDeltaPaths = apilibadmin.DeltaUnionDeltaPaths

// AssembleDeltaUnionExpectations builds one expectation per scope; it
// delegates to the single apilib implementation.
func AssembleDeltaUnionExpectations(
	rows []DeltaUnionActiveRow,
	deltas map[string]DeltaUnionDeltaPaths,
) []DeltaUnionExpectation {
	return apilibadmin.AssembleDeltaUnionExpectations(rows, deltas)
}

// DeltaUnionRepoOutcome is one repository's completeness verdict.
type DeltaUnionRepoOutcome = apilibadmin.DeltaUnionRepoOutcome

const (
	// DeltaUnionRepoComplete means the graph holds exactly the ACTIVE set.
	DeltaUnionRepoComplete = apilibadmin.DeltaUnionRepoComplete
	// DeltaUnionRepoMismatch means the graph differs from the ACTIVE set.
	DeltaUnionRepoMismatch = apilibadmin.DeltaUnionRepoMismatch
	// DeltaUnionRepoSkipped means the delta-union comparison does not apply.
	DeltaUnionRepoSkipped = apilibadmin.DeltaUnionRepoSkipped
)

// DeltaUnionRepoResult is the structured per-repository comparison.
type DeltaUnionRepoResult = apilibadmin.DeltaUnionRepoResult

// CompareDeltaUnionRepo compares one repository's GRAPH side against its
// ACTIVE side; it delegates to the single apilib implementation.
func CompareDeltaUnionRepo(exp DeltaUnionExpectation, obs DeltaUnionObservation) DeltaUnionRepoResult {
	return apilibadmin.CompareDeltaUnionRepo(exp, obs)
}

// EvaluateDeltaUnionCompleteness compares each repository's GRAPH side
// against its ACTIVE side and records one required check per repository. The
// detail carries the generation-state annotation (base and active
// generations) so a failure names the exact generations to repair.
func EvaluateDeltaUnionCompleteness(
	expected []DeltaUnionExpectation,
	observed []DeltaUnionObservation,
	r *Report,
) {
	byRepo := make(map[string]DeltaUnionObservation, len(observed))
	for _, obs := range observed {
		byRepo[obs.RepoID] = obs
	}
	for _, exp := range expected {
		result := CompareDeltaUnionRepo(exp, byRepo[exp.RepoID])
		switch result.Outcome {
		case DeltaUnionRepoSkipped:
			reason := "no active generation"
			if exp.ActiveGenerationID != "" {
				reason = "active generation is not a delta"
			}
			r.AddCheck("delta-union", "repo_"+exp.RepoID, true, false,
				fmt.Sprintf("skip: %s (%s)", reason, result.Annotation))
		case DeltaUnionRepoComplete:
			r.AddCheck("delta-union", "repo_"+exp.RepoID, true, true,
				fmt.Sprintf("complete: %d files agree (%s)", len(exp.Files), result.Annotation))
		default:
			r.AddCheck("delta-union", "repo_"+exp.RepoID, false, true,
				fmt.Sprintf("mismatch: missing=%v extra=%v drifted=%v (%s)",
					result.Missing, result.Extra, result.Drifted, result.Annotation))
		}
	}
}
