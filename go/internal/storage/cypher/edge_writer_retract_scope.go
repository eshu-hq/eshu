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

// wholeScopeRetractDomains splits the domains that reducer's
// domainHasRepoWideRetract fences (shared_projection_worker_refresh_fence.go)
// into the two groups RetractEdges treats differently. It is the ONE place
// either group is written down: retractFencedRepoWideDomain
// (edge_writer_retract.go) gates on isWholeScopeNarrowedDomain and reaches the
// narrowed half through narrowedWholeScopeRepoIDs, and the nil-fence and
// fenced-but-not-narrowed tests loop over the halves instead of over a
// hand-typed literal, so the size of either group is never stated in prose
// independently of this table.
//
// true -- NARROWED (#6166). The whole-scope retract binds
// collectWholeScopeRefreshRepoIDs, so a batch whose rows carry no refresh
// intent_type contributes no repository id, and the whole-repository DELETE is
// skipped rather than run over the batch-wide list. Their retract rows come
// from planRepoWideRetractWork, which also routes unmarked legacy per-edge rows
// into the retract; binding one of those to a whole-repository DELETE erases a
// repository's edges across every file while only this batch's rows get
// rewritten.
//
// false -- FENCED BUT NOT NARROWED. These fall through to buildRetractStatement
// with the batch-wide collectRepoIDs list, and must keep doing so.
//
// Both halves live in one table because re-deriving either from the fencing
// predicate gives the WRONG answer. domainHasRepoWideRetract returns true for
// all seven rows below, so a reader who counts narrowed domains under it gets
// seven; only a reader who walks collectWholeScopeRefreshRepoIDs' non-test call
// sites can tell the halves apart. Written down once, that is not a derivation
// anyone has to repeat or get wrong.
//
// DomainCodeCalls is deliberately in NEITHER half. It looks like a narrowed
// sibling and is not one: it is absent from domainHasRepoWideRetract, its rows
// never pass through planRepoWideRetractWork, and requiring the refresh
// intent_type on them empties repoIDs and stops the code-call retract running
// at all. Read the note at its branch in RetractEdges before adding it here for
// symmetry.
//
// Adding a row: a true row needs a branch in retractFencedRepoWideDomain that
// routes through narrowedWholeScopeRepoIDs -- without one that function returns
// an error naming this table, and
// TestRetractEdgesNilFenceShapeSkipsWholeScopeDelete fails for it. A false row must NOT have one, or
// TestFencedButNotNarrowedDomainsStillBindBatchWideRepoIDs fails for it.
// Narrowing a domain without registering it here fails the same way.
//
// MISSING a row fails too, which it did not use to. The rows must be exactly the
// domains reducer.RepoWideRetractDomains() returns, and
// TestWholeScopeRetractDomainsCoversFencedSet compares the two sets in both
// directions -- an eighth fenced domain left out of this table would otherwise
// re-introduce the #6166 over-delete with every test in that file green,
// because no loop here would ever reach it.
var wholeScopeRetractDomains = map[string]bool{
	reducer.DomainInheritanceEdges:   true,
	reducer.DomainRationaleEdges:     true,
	reducer.DomainSQLRelationships:   true,
	reducer.DomainShellExec:          true,
	reducer.DomainHandlesRoute:       false,
	reducer.DomainRunsIn:             false,
	reducer.DomainInvokesCloudAction: false,
}

// wholeScopeNarrowedDomains returns the narrowed half of
// wholeScopeRetractDomains in a stable order. Map iteration order is
// randomised, so a caller that subtests or reports on the result would
// otherwise vary run to run.
func wholeScopeNarrowedDomains() []string {
	return wholeScopeDomainsWhereNarrowed(true)
}

// wholeScopeUnnarrowedDomains returns the fenced-but-not-narrowed half of
// wholeScopeRetractDomains in a stable order.
func wholeScopeUnnarrowedDomains() []string {
	return wholeScopeDomainsWhereNarrowed(false)
}

// wholeScopeDomainsWhereNarrowed returns the half of wholeScopeRetractDomains
// whose narrowing flag equals narrowed, sorted.
func wholeScopeDomainsWhereNarrowed(narrowed bool) []string {
	domains := make([]string, 0, len(wholeScopeRetractDomains))
	for domain, isNarrowed := range wholeScopeRetractDomains {
		if isNarrowed == narrowed {
			domains = append(domains, domain)
		}
	}
	sort.Strings(domains)
	return domains
}

// isWholeScopeNarrowedDomain reports whether a domain's whole-scope retract
// narrows to the refresh-marked repository ids rather than the batch-wide list.
func isWholeScopeNarrowedDomain(domain string) bool {
	return wholeScopeRetractDomains[domain]
}

// narrowedWholeScopeRepoIDs is the one sanctioned way a retract branch narrows
// its whole-scope repository ids, and the only place logWholeScopeRetractSkipped
// is called from.
//
// It returns the repository ids the caller may bind, and skip=true when the
// batch carried no refresh-marked row at all -- the anomalous shape where the
// whole-repository DELETE must not run. The caller returns nil on a skip; the
// warning is logged here because that early return never reaches
// recordGroupedWrite and would otherwise leave no trace anywhere.
//
// A domain outside the narrowed half of wholeScopeRetractDomains is a
// programming error, and it returns an ERROR rather than skipping. Skipping
// would be a silent lost retract -- the exact failure this mechanism exists to
// make visible -- while an error surfaces through the batch and names the fix.
// Routing all four branches through here is what binds the dispatch to the
// table: a fifth branch either registers its domain or fails on first use.
func (w *EdgeWriter) narrowedWholeScopeRepoIDs(
	domain string,
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) (repoIDs []string, skip bool, err error) {
	if !isWholeScopeNarrowedDomain(domain) {
		return nil, false, fmt.Errorf(
			"whole-scope retract narrowing requested for domain %q, which is not in the narrowed half of wholeScopeRetractDomains (edge_writer_retract_scope.go); register it there so the nil-fence and logging contracts cover it",
			domain,
		)
	}
	repoIDs = collectWholeScopeRefreshRepoIDs(rows)
	if len(repoIDs) == 0 {
		w.logWholeScopeRetractSkipped(domain, evidenceSource, len(rows))
		return nil, true, nil
	}
	return repoIDs, false, nil
}
