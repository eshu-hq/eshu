// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// This file holds the row/scope collection helpers that feed RetractEdges
// (edge_writer_retract.go): extracting repo ids, scope ids, delta file paths,
// and documentation delta scope from retract rows, plus the documentation
// delta statement builder that consumes that scope.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// collectRepoIDs returns every distinct repository id in the batch, with NO
// intent_type filter. It now serves only the REPO-KEYED retract domains: code
// calls, repo dependency, submodule pin, codeowners ownership, workload
// dependency, and the remaining buildRetractStatement cases.
//
// The missing filter is required, not an oversight, and #6166 measured what
// adding one costs. Every one of those domains synthesises its retract rows in
// the caller rather than draining them from the shared-projection queue --
// buildCodeCallRepoRetractRows (reducer/code_call_projection_work.go),
// buildRepoDependencyRetractRows (reducer/repo_dependency_projection_replay.go),
// buildSubmodulePinRepoRetractRows (reducer/submodule_pin_delta_scope.go), the
// nil-payload rows codeowners selects on
// (reducer/codeowners_ownership_materialization.go), and the workload
// dependency reconcile rows -- and none of them carries an intent_type,
// because none of them came from a refresh intent. Requiring
// reducer.RepoRefreshIntentType here empties the bound repo_ids for all of
// them and every one of those retracts silently stops running, leaving stale
// edges with no error and no dead letter. That is a worse failure than the
// over-delete it would prevent: a retract that no longer fires is wrong graph
// truth, and wrong graph truth is a product failure.
//
// The four FENCED repo-wide-retract domains do NOT use this collector on their
// whole-scope path any more; they use collectWholeScopeRefreshRepoIDs. The
// difference between the two groups is where the rows come from, not what the
// DELETE looks like. See RetractEdges (edge_writer_retract.go) for the split.
func collectRepoIDs(rows []reducer.SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var result []string
	for _, row := range rows {
		repoID := row.RepositoryID
		if repoID == "" {
			repoID = payloadString(row.Payload, "repo_id")
		}
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		result = append(result, repoID)
	}
	return result
}

// collectWholeScopeRefreshRepoIDs is collectRepoIDs restricted to repo-wide
// REFRESH rows that are not delta-scoped. Every whole-scope retract in a
// FENCED repo-wide-retract domain binds this collector: the rationale
// mixed-batch branch it was written for, and -- since #6166 -- the non-delta
// branches of inheritance, rationale, SQL relationships and shell exec.
//
// #6166: those four non-delta branches used to bind the batch-wide
// collectRepoIDs. planRepoWideRetractWork routes unmarked legacy per-edge rows
// into the retract alongside the refresh rows
// (reducer/shared_projection_worker_refresh_fence.go), so one such row handed
// its repository a whole-repository DELETE that erased its edges across every
// file while only that batch's rows were rewritten. Each of the four domains
// now carries a reachability test proving no current emitter can produce an
// unmarked row -- every per-edge intent is stamped retract_via_refresh at
// emission -- so the narrowing is a no-op on today's input and a guard for the
// day an emitter changes. Narrowing the shared collectRepoIDs instead was
// measured and rejected; see its doc for the five domains it breaks.
//
// It was originally written for the rationale EXPLAINS
// domain's mixed-batch retract (#5998 review F6): one ProcessPartitionOnce
// batch can legitimately contain some repositories' refresh rows correctly
// flagged delta_projection:true (buildRationaleRefreshIntents,
// go/internal/reducer/rationale_edge_intents.go, gates that flag per
// repository) alongside sibling repositories' refresh rows that carry no
// delta_projection key at all -- a repository on a full generation whose scope
// happens to share a partition bucket with a delta-generation sibling.
// collectDeltaFilePaths only ever sees the delta-flagged rows, so the
// whole-scope refresh rows this function collects need their own retract;
// otherwise those repositories are silently dropped from the batch whenever
// hasDeltaScope ends up true because of an unrelated sibling row.
//
// Both conditions are load-bearing, and the intent_type one is the subtle one.
// "Lacks delta_projection" is NOT the same as "is a whole-scope refresh": a
// batch can also carry unmarked legacy per-edge rows, which
// planRepoWideRetractWork deliberately routes into retractRows so they drain
// instead of deferring forever (shared_projection_worker_refresh_fence.go), and
// ProcessPartitionOnce passes every row as retractRows when no refresh fence is
// configured at all. Those rows carry no delta_projection either. Sweeping them
// in here would hand their repository a whole-repository
// `rationale.repo_id IN $repo_ids` DELETE that erases that repository's entire
// EXPLAINS set across every file, while only this batch's rows get rewritten --
// a strictly new over-delete that the pre-#5998 file-scoped path never
// performed. Requiring the refresh intent_type keeps the whole-repository
// delete bound to rows that actually asked for a whole-repository refresh.
//
// #6165 review F1: this function and its sibling collectDeltaProjectionRepoIDs
// are not provably disjoint from each other's output by anything IN THIS FILE
// -- neither collector is aware of the other's rows. They are disjoint today
// only because of an UPSTREAM property: reducer.LatestIntentsByRepoAndPartition
// dedupes intents by (acceptance key, partition key), and every rationale
// whole-scope refresh -- delta-flagged or not -- is emitted under the same
// per-repository partition key (rationaleWholeScopePartitionKey, which carries
// no delta/generation component), so at most one refresh row per repository
// survives a batch.
//
// TWO upstream mechanisms carry that, not one, and BOTH must hold.
// reducer.FilterAuthoritativeIntents runs first and keeps only rows matching the
// accepted generation for their acceptance key, so every row that reaches dedup
// for a repository already shares one (scope, unit, run) tuple; the shared
// partition key then collapses that set to a single survivor. The shared
// partition key ALONE is not sufficient -- two refresh rows for one repository
// differing in SourceRunID would both survive dedup on their own.
//
// This therefore breaks if either mechanism moves, and the two ways it breaks
// fail in OPPOSITE directions.
//
// Over-retract: a future change giving the delta variant its own partition key
// (or, in principle, two accepted generations live at once for one acceptance
// key -- today the AcceptedGenerationLookup signature returns a single
// generation per key, so that one is type-enforced rather than merely intended).
// Then both rows survive dedup, reach this file, and the repository lands in
// BOTH collectors' output: its delta files get rewritten AND its entire EXPLAINS
// set is deleted repo-wide in the same batch, silently -- no error, no dead
// letter. The reducer pins the key shape against the first case in
// TestRepoWideRetractRefreshPartitionKeyShapeIsPinned.
//
// Lost retract: dedup reordered ahead of the authoritative filter. This one does
// NOT produce the over-delete above -- it drops work instead. GenerationID is not
// part of reducer's dedup key (scope, acceptance unit, source run, repository,
// partition key), and SourceRunID and GenerationID are independent fields, so two
// rows for one repository can share a dedup key while differing in generation.
// Filtering first drops the stale-generation row and dedup then sees only the
// accepted one. Deduping first collapses the pair to a single survivor chosen by
// refresh-first, then LATEST CreatedAt, then largest intent id -- the STALE row
// can be the one that survives,
// which the filter then discards. The repository loses its refresh for that
// cycle: the whole-scope retract never runs and stale EXPLAINS edges persist,
// again with no error and no dead letter. The order is not enforced by the
// compiler -- both functions take and return the same slice type, so the swap
// type-checks -- so reducer pins it in
// TestSelectPartitionBatchFiltersBeforeDeduping. See
// TestCollectDeltaAndWholeScopeRefreshRepoIDsStayDisjointAfterDedup, which
// asserts both the collapse and the production-shaped acceptance key it needs.
func collectWholeScopeRefreshRepoIDs(rows []reducer.SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var result []string
	for _, row := range rows {
		if payloadBool(row.Payload, "delta_projection") {
			continue
		}
		if payloadString(row.Payload, "intent_type") != reducer.RepoRefreshIntentType {
			continue
		}
		repoID := row.RepositoryID
		if repoID == "" {
			repoID = payloadString(row.Payload, "repo_id")
		}
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		result = append(result, repoID)
	}
	return result
}

// collectDeltaProjectionRepoIDs is collectRepoIDs restricted to the rows the
// delta path actually retracts on. The delta guard reports repo_count and one
// sample_repo_id on every probe outcome, so handing it the whole batch would
// let a delta_by_file_path log line name a repository whose retract went
// through the whole-scope call instead.
func collectDeltaProjectionRepoIDs(rows []reducer.SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var result []string
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		repoID := row.RepositoryID
		if repoID == "" {
			repoID = payloadString(row.Payload, "repo_id")
		}
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		result = append(result, repoID)
	}
	return result
}

// collectScopeIDs gathers the durable scope ids carried by retract rows,
// deduped and order-preserving. Documentation edges anchor every retract on
// section.scope_id, so the retract must bind the row's scope id (preferring the
// ScopeID field, falling back to the payload scope_id) rather than its
// repository id. Blank ids are skipped.
func collectScopeIDs(rows []reducer.SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var result []string
	for _, row := range rows {
		scopeID := strings.TrimSpace(row.ScopeID)
		if scopeID == "" {
			scopeID = strings.TrimSpace(payloadString(row.Payload, "scope_id"))
		}
		if scopeID == "" {
			continue
		}
		if _, ok := seen[scopeID]; ok {
			continue
		}
		seen[scopeID] = struct{}{}
		result = append(result, scopeID)
	}
	return result
}

// retractRowRepositoryID returns a retract row's repository id, preferring the
// typed field and falling back to the payload, matching how every collector in
// this file resolves it. It exists so a fail-closed error can name the
// repository an operator has to look at.
func retractRowRepositoryID(row reducer.SharedProjectionIntentRow) string {
	if row.RepositoryID != "" {
		return row.RepositoryID
	}
	return payloadString(row.Payload, "repo_id")
}

// collectDeltaFilePaths gathers the file paths a delta-scoped retract binds, and
// FAILS CLOSED on a delta-flagged row that carries none.
//
// That rejection is load-bearing, not defensive tidiness (#6216). A repository
// on a delta generation had content-entity facts emitted for its CHANGED files
// only, so the generation's writes re-create only those files' edges. There is
// no correct retract to run for it here: the file-scoped one has nothing to
// bind, and the repo-wide one would delete every UNCHANGED file's edge with
// nothing left to restore it. Failing the partition dead-letters the intent,
// which an operator can see; the alternative loses graph edges silently. The
// error names the repository so the dead letter is actionable -- the usual cause
// is a repository fact with no local_path, or a symlinked repos root whose
// changed paths normalizeSnapshotRelativePaths drops.
func collectDeltaFilePaths(rows []reducer.SharedProjectionIntentRow) ([]string, bool, error) {
	seen := make(map[string]struct{})
	hasDeltaScope := false
	deltaRowCount := 0
	var filePaths []string
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDeltaScope = true
		deltaRowCount++
		rowFilePaths := payloadStringSlice(row.Payload, "delta_file_paths")
		if len(rowFilePaths) == 0 {
			return nil, true, fmt.Errorf(
				"delta retract requires delta_file_paths: repository %q carries none",
				retractRowRepositoryID(row),
			)
		}
		for _, filePath := range rowFilePaths {
			filePath = strings.TrimSpace(filePath)
			if filePath == "" {
				continue
			}
			if _, ok := seen[filePath]; ok {
				continue
			}
			seen[filePath] = struct{}{}
			filePaths = append(filePaths, filePath)
		}
	}
	if hasDeltaScope && len(filePaths) == 0 {
		return nil, true, fmt.Errorf(
			"delta retract requires delta_file_paths: no path survived across %d delta-flagged row(s)",
			deltaRowCount,
		)
	}
	sort.Strings(filePaths)
	return filePaths, hasDeltaScope, nil
}

type documentationRetractScope struct {
	documentIDs []string
	sectionUIDs []string
}

func collectDocumentationDeltaScope(rows []reducer.SharedProjectionIntentRow) (documentationRetractScope, bool, error) {
	seenDocuments := make(map[string]struct{})
	seenSections := make(map[string]struct{})
	hasDeltaScope := false
	scope := documentationRetractScope{}
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDeltaScope = true
		rowDocumentIDs := payloadStringSlice(row.Payload, "document_ids")
		for _, documentID := range rowDocumentIDs {
			documentID = strings.TrimSpace(documentID)
			if documentID == "" {
				continue
			}
			if _, ok := seenDocuments[documentID]; ok {
				continue
			}
			seenDocuments[documentID] = struct{}{}
			scope.documentIDs = append(scope.documentIDs, documentID)
		}
		for _, sectionUID := range payloadStringSlice(row.Payload, "section_uids") {
			sectionUID = strings.TrimSpace(sectionUID)
			if sectionUID == "" {
				continue
			}
			if _, ok := seenSections[sectionUID]; ok {
				continue
			}
			seenSections[sectionUID] = struct{}{}
			scope.sectionUIDs = append(scope.sectionUIDs, sectionUID)
		}
	}
	if hasDeltaScope && len(scope.documentIDs) == 0 && len(scope.sectionUIDs) == 0 {
		return documentationRetractScope{}, true, fmt.Errorf("documentation delta retract requires document_ids or section_uids")
	}
	sort.Strings(scope.documentIDs)
	sort.Strings(scope.sectionUIDs)
	return scope, hasDeltaScope, nil
}

func buildDocumentationDeltaRetractStatements(
	scopeIDs []string,
	deltaScope documentationRetractScope,
	evidenceSource string,
) []Statement {
	stmts := make([]Statement, 0, 2)
	if len(deltaScope.sectionUIDs) > 0 {
		stmts = append(stmts, BuildRetractDocumentationEdgesBySectionUID(
			scopeIDs,
			deltaScope.sectionUIDs,
			evidenceSource,
		))
	}
	if len(deltaScope.documentIDs) > 0 {
		stmts = append(stmts, BuildRetractDocumentationEdgesByDocumentID(
			scopeIDs,
			deltaScope.documentIDs,
			evidenceSource,
		))
	}
	return stmts
}
