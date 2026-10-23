// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"fmt"
	"sort"
	"strings"
)

// DeltaUnionExpectation is one repository's ACTIVE side: the file set the
// graph must hold, assembled by AssembleDeltaUnionExpectations from the
// base-full generation overlaid with the active delta through an independent
// SQL-level implementation, never through the projection's
// facts.OverlayDeltaBase, so one bug cannot hide in both. Files maps a repo
// path to its expected entity count.
type DeltaUnionExpectation struct {
	// RepoID and ScopeID identify the repository and its scope.
	RepoID  string
	ScopeID string
	// BaseGenerationID names the base full generation, "" when the active
	// generation is itself full. BaseStatus is its status (active,
	// completed, or superseded).
	BaseGenerationID string
	BaseStatus       string
	// ActiveGenerationID names the generation the graph must reflect, "" when
	// the scope has no active generation (the check skips with a reason).
	// ActiveIsDelta tells whether it carries only file-scoped changes.
	ActiveGenerationID string
	ActiveIsDelta      bool
	// Files maps repo paths to expected entity counts.
	Files map[string]int64
}

// DeltaUnionObservation is one repository's GRAPH side: the file set the
// graph actually holds, with per-path entity counts.
type DeltaUnionObservation struct {
	// RepoID identifies the repository.
	RepoID string
	// Files maps repo paths to observed entity counts.
	Files map[string]int64
}

// DeltaUnionActiveRow is one fact row behind the ACTIVE side: the generation
// it came from, its kind and stable key, its file path ("" when the kind is
// not file-attributable), and whether it is a tombstone. Tombstoned-only
// rows never count on either side.
type DeltaUnionActiveRow struct {
	// RepoID and ScopeID identify the repository and its scope.
	RepoID  string
	ScopeID string
	// GenerationID and GenerationStatus identify the row's generation.
	// IsDelta tells whether the generation carries only file-scoped changes.
	GenerationID     string
	GenerationStatus string
	IsDelta          bool
	// Kind, StableKey, Path, and Tombstone describe the fact.
	Kind      string
	StableKey string
	Path      string
	Tombstone bool
	// Entity counts one Function content_entity row toward its path. The
	// ACTIVE read sets it only for entity_type function/Function, the
	// entity types the projection writes as :Function nodes under the
	// file, so the want side counts the same population the graph
	// readers count ((f)-[:CONTAINS]->(e:Function)).
	Entity bool
}

// DeltaUnionDeltaPaths carries one active delta's touched paths, read from
// its repository fact independently of the union function.
type DeltaUnionDeltaPaths struct {
	// BaselineCommit is the commit the delta was diffed from.
	BaselineCommit string
	// Changed and Deleted are the delta's touched repo paths.
	Changed []string
	Deleted []string
}

// deltaUnionFileKinds lists the file-attributable kinds this assembly's
// database read carries a file path for, so the independent assembly drops
// base rows under touched paths the same way the union does. The union drops
// base rows for more kinds (the git observability kinds, #8780); those rows
// reach this read without a file path and are skipped either way, so they
// are not listed here. It is a deliberate second spelling of the rule, not a
// call into it.
var deltaUnionFileKinds = map[string]bool{
	"file": true, "content_entity": true, "parsed_file_data": true, "content": true,
}

// AssembleDeltaUnionExpectations builds one expectation per scope from raw
// ACTIVE-side rows: the active generation's rows overlaid on its base full
// generation's rows. The delta wins per stable key, tombstones remove the
// same-key base row, and base rows of file-attributable kinds under the
// delta's touched paths are dropped. Scopes with no active generation row
// still yield an expectation with an empty ActiveGenerationID so the check
// skips them with a reason instead of failing.
func AssembleDeltaUnionExpectations(
	rows []DeltaUnionActiveRow,
	deltas map[string]DeltaUnionDeltaPaths,
) []DeltaUnionExpectation {
	byScope := make(map[string][]DeltaUnionActiveRow)
	order := []string{}
	for _, row := range rows {
		if _, ok := byScope[row.ScopeID]; !ok {
			order = append(order, row.ScopeID)
		}
		byScope[row.ScopeID] = append(byScope[row.ScopeID], row)
	}
	for scopeID := range deltas {
		if _, ok := byScope[scopeID]; !ok {
			byScope[scopeID] = nil
			order = append(order, scopeID)
		}
	}
	sort.Strings(order)
	out := make([]DeltaUnionExpectation, 0, len(order))
	for _, scopeID := range order {
		out = append(out, assembleDeltaUnionScope(scopeID, byScope[scopeID], deltas[scopeID]))
	}
	return out
}

func assembleDeltaUnionScope(scopeID string, rows []DeltaUnionActiveRow, delta DeltaUnionDeltaPaths) DeltaUnionExpectation {
	exp := DeltaUnionExpectation{ScopeID: scopeID, Files: map[string]int64{}}
	var (
		activeRows []DeltaUnionActiveRow
		baseRows   []DeltaUnionActiveRow
	)
	for _, row := range rows {
		exp.RepoID = row.RepoID
		if row.GenerationStatus != "active" {
			continue
		}
		if exp.ActiveGenerationID == "" {
			exp.ActiveGenerationID = row.GenerationID
			exp.ActiveIsDelta = row.IsDelta
		}
		if row.GenerationID == exp.ActiveGenerationID {
			activeRows = append(activeRows, row)
		}
	}
	if exp.ActiveGenerationID == "" {
		return exp
	}
	if exp.ActiveIsDelta {
		for _, row := range rows {
			if !row.IsDelta && row.GenerationID != exp.ActiveGenerationID {
				if exp.BaseGenerationID == "" {
					exp.BaseGenerationID = row.GenerationID
					exp.BaseStatus = row.GenerationStatus
				}
				baseRows = append(baseRows, row)
			}
		}
	}
	deltaKeys := make(map[string]bool, len(activeRows))
	for _, row := range activeRows {
		if key := strings.TrimSpace(row.StableKey); key != "" {
			deltaKeys[key] = true
		}
	}
	touched := make(map[string]bool, len(delta.Changed)+len(delta.Deleted))
	for _, p := range delta.Changed {
		touched[p] = true
	}
	for _, p := range delta.Deleted {
		touched[p] = true
	}
	// The merge key is the stable key alone, matching the union: a delta row
	// of any kind replaces the same-key base row. Empty keys never match.
	merged := make(map[string]DeltaUnionActiveRow)
	emptySeq := 0
	merge := func(row DeltaUnionActiveRow) {
		key := strings.TrimSpace(row.StableKey)
		if key == "" {
			emptySeq++
			key = fmt.Sprintf("\x00empty\x00%d", emptySeq)
		}
		merged[key] = row
	}
	for _, row := range baseRows {
		if row.Tombstone {
			continue
		}
		if key := strings.TrimSpace(row.StableKey); key != "" && deltaKeys[key] {
			continue
		}
		if deltaUnionFileKinds[row.Kind] && touched[row.Path] {
			continue
		}
		merge(row)
	}
	for _, row := range activeRows {
		if row.Tombstone {
			continue
		}
		merge(row)
	}
	// The file set is file-kind authoritative; entity rows only count. Other
	// kinds (content, parsed_file_data) ride on their file's row.
	for _, row := range merged {
		if row.Path == "" {
			continue
		}
		if row.Kind == "file" {
			if _, ok := exp.Files[row.Path]; !ok {
				exp.Files[row.Path] = 0
			}
		}
		if row.Entity {
			exp.Files[row.Path]++
		}
	}
	return exp
}

// DeltaUnionRepoOutcome is one repository's completeness verdict.
type DeltaUnionRepoOutcome string

const (
	// DeltaUnionRepoComplete means the graph holds exactly the ACTIVE set.
	DeltaUnionRepoComplete DeltaUnionRepoOutcome = "complete"
	// DeltaUnionRepoMismatch means the graph differs from the ACTIVE set.
	DeltaUnionRepoMismatch DeltaUnionRepoOutcome = "mismatch"
	// DeltaUnionRepoSkipped means the scope has no active generation, or
	// its active generation is not a delta, so the delta-union comparison
	// does not apply to it.
	DeltaUnionRepoSkipped DeltaUnionRepoOutcome = "skipped"
)

// DeltaUnionRepoResult is the structured per-repository comparison the gate
// phase records and the prod admin surface serves.
type DeltaUnionRepoResult struct {
	// RepoID and ScopeID identify the repository and its scope.
	RepoID  string
	ScopeID string
	// Outcome is the verdict.
	Outcome DeltaUnionRepoOutcome
	// Missing lists ACTIVE paths absent from the graph, Extra lists graph
	// paths absent from ACTIVE, Drifted lists paths whose entity counts
	// disagree as "path: graph=N want=M".
	Missing []string
	Extra   []string
	Drifted []string
	// Annotation names the generations behind the verdict for repair.
	Annotation string
}

// CompareDeltaUnionRepo compares one repository's GRAPH side against its
// ACTIVE side: pass when the file sets and per-path entity counts agree,
// mismatch on any missing, extra, or count-drifted path, skip when the scope
// has no active generation or its active generation is not a delta (the
// check covers delta-active scopes; a full active generation is out of this
// comparison's scope). Empty delta-active repositories pass.
func CompareDeltaUnionRepo(exp DeltaUnionExpectation, obs DeltaUnionObservation) DeltaUnionRepoResult {
	result := DeltaUnionRepoResult{
		RepoID:  exp.RepoID,
		ScopeID: exp.ScopeID,
		Annotation: fmt.Sprintf("repo=%s scope=%s base=%s(%s) active=%s(delta=%v)",
			exp.RepoID, exp.ScopeID, exp.BaseGenerationID, exp.BaseStatus,
			exp.ActiveGenerationID, exp.ActiveIsDelta),
	}
	if exp.ActiveGenerationID == "" {
		result.Outcome = DeltaUnionRepoSkipped
		return result
	}
	if !exp.ActiveIsDelta {
		result.Outcome = DeltaUnionRepoSkipped
		return result
	}
	for path, count := range exp.Files {
		got, ok := obs.Files[path]
		switch {
		case !ok:
			result.Missing = append(result.Missing, path)
		case got != count:
			result.Drifted = append(result.Drifted, fmt.Sprintf("%s: graph=%d want=%d", path, got, count))
		}
	}
	for path := range obs.Files {
		if _, ok := exp.Files[path]; !ok {
			result.Extra = append(result.Extra, path)
		}
	}
	sort.Strings(result.Missing)
	sort.Strings(result.Extra)
	sort.Strings(result.Drifted)
	result.Outcome = DeltaUnionRepoComplete
	if len(result.Missing) != 0 || len(result.Extra) != 0 || len(result.Drifted) != 0 {
		result.Outcome = DeltaUnionRepoMismatch
	}
	return result
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
