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
// intent_type filter. That asymmetry with its sibling
// collectWholeScopeRefreshRepoIDs is deliberate and load-bearing to know
// about: the sibling requires reducer.RepoRefreshIntentType precisely so an
// unmarked legacy per-edge row cannot pull a whole-repository retract, while
// this function is the pre-#5998 batch-wide collector and still can. A
// no-delta batch carrying such a row therefore still reaches a repo-wide
// DELETE for that repository. That is pre-existing behavior, unchanged by the
// #5998 probe guard and deliberately left alone by it, and it is tracked in
// #6166 -- do not read the sibling's guard as covering this path too.
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
// REFRESH rows that are not delta-scoped. It exists for the rationale EXPLAINS
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
// This therefore breaks if EITHER mechanism moves: a future change giving the
// delta variant its own partition key, two accepted generations live at once for
// one acceptance key, or dedup reordered ahead of the authoritative filter. In
// any of those cases both rows survive and reach this file, and a repository
// lands in both collectors' output: its delta files get rewritten AND its entire
// EXPLAINS set is deleted repo-wide in the same batch, silently -- no error, no
// dead letter. See
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

func collectDeltaFilePaths(rows []reducer.SharedProjectionIntentRow) ([]string, bool, error) {
	seen := make(map[string]struct{})
	hasDeltaScope := false
	var filePaths []string
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDeltaScope = true
		rowFilePaths := payloadStringSlice(row.Payload, "delta_file_paths")
		if len(rowFilePaths) == 0 {
			return nil, true, fmt.Errorf("delta retract requires delta_file_paths")
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
		return nil, true, fmt.Errorf("delta retract requires delta_file_paths")
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
