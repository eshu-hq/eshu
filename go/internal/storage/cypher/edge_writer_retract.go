// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	canonicalRationaleEvidenceSource = "reducer/rationale"
	legacyRationaleEvidenceSource    = "finalization/workloads"
)

func rationaleRetractEvidenceSources(evidenceSource string) []string {
	if evidenceSource != canonicalRationaleEvidenceSource {
		return []string{evidenceSource}
	}
	return []string{evidenceSource, legacyRationaleEvidenceSource}
}

// RetractEdges retracts canonical domain edges for the given rows. Retraction
// collects repo IDs from all rows and executes one batched DELETE statement,
// except for the domains special-cased below (delta-scoped, per-source-label,
// or scope-anchored retracts) whose Cypher shape differs from the single
// repo-id-bound statement buildRetractStatement returns.
func (w *EdgeWriter) RetractEdges(
	ctx context.Context,
	domain string,
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("edge writer executor is required")
	}

	if domain == reducer.DomainCodeCalls {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmts := BuildRetractCodeCallEdgeStatementsByFilePath(filePaths, evidenceSource)
			return w.executeCodeCallRetractStatements(ctx, stmts)
		}
	}

	if handled, err := w.retractFencedRepoWideDomain(ctx, domain, rows, evidenceSource); handled {
		return err
	}

	if domain == reducer.DomainDocumentationEdges {
		// Documentation is scope-scoped: every retract anchors on
		// section.scope_id, so the durable owner is the row's scope id (not its
		// repository id). Thread collectScopeIDs here for both the delta and the
		// whole-scope path to keep the partition-key dimension aligned with the
		// retract anchor.
		scopeIDs := collectScopeIDs(rows)
		deltaScope, hasDeltaScope, err := collectDocumentationDeltaScope(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmts := buildDocumentationDeltaRetractStatements(scopeIDs, deltaScope, evidenceSource)
			return w.executeDocumentationRetractStatements(ctx, stmts)
		}
		return WrapRetryableNeo4jError(
			w.executor.Execute(ctx, BuildRetractDocumentationEdges(scopeIDs, evidenceSource)),
		)
	}

	// Two groups share the batch-wide repoIDs below, and which group a domain
	// is in decides whether narrowing it is a fix or a lost retract (#6166).
	// Check the group before changing any call here.
	//
	// REPO-KEYED domains keep the batch-wide list. Their retract rows are
	// synthesised per repository by the caller and carry no intent_type at
	// all, so filtering on one empties the list and the retract silently stops
	// running -- measured across code calls, repo dependency, submodule pin,
	// codeowners and workload dependency. See the code-call note below.
	//
	// FENCED repo-wide-retract domains never reach here: retractFencedRepoWideDomain
	// above handled them, narrowing to collectWholeScopeRefreshRepoIDs. WHICH
	// domains those are is the narrowed half of wholeScopeRetractDomains
	// (edge_writer_retract_scope.go), never a count restated here (#6276).
	repoIDs := collectRepoIDs(rows)
	if domain == reducer.DomainCodeCalls {
		// Deliberately the batch-wide repoIDs, and NOT the narrowing its
		// fenced siblings apply -- this branch looks like them and is not one
		// (#6166). DomainCodeCalls is in NEITHER half of
		// wholeScopeRetractDomains, and absent from
		// domainHasRepoWideRetract, so its rows never pass through
		// planRepoWideRetractWork at all; they are synthesised by
		// buildCodeCallRepoRetractRows (reducer/code_call_projection_work.go),
		// which emits a bare {"repo_id": ...} payload with no intent_type.
		// Requiring the refresh intent_type here empties repoIDs and the
		// code-call retract stops running entirely, leaving stale CALLS edges
		// behind. Do not "fix" this for symmetry with its neighbours.
		stmts := BuildRetractCodeCallEdgeStatements(repoIDs, evidenceSource)
		return w.executeCodeCallRetractStatements(ctx, stmts)
	}
	if domain == reducer.DomainRepoDependency {
		return w.executeRepoDependencyRetractStatements(ctx, repoIDs, evidenceSource)
	}
	if domain == reducer.DomainCodeownersOwnershipEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmt := BuildRetractCodeownersOwnershipEdgesByFilePath(repoIDs, filePaths, evidenceSource)
			return WrapRetryableNeo4jError(w.executor.Execute(ctx, stmt))
		}
	}

	stmt, err := buildRetractStatement(domain, repoIDs, evidenceSource)
	if err != nil {
		return err
	}

	return WrapRetryableNeo4jError(w.executor.Execute(ctx, stmt))
}

// executeCodeCallRetractStatements runs the per-source-label code-call retract
// statements (#5116) sequentially, each in its own transaction — deliberately
// NOT grouped through ExecuteGroup.
//
// On NornicDB v1.1.11 multiple DELETE statements sharing a single managed
// transaction do not all apply: the grouped per-label retract leaves some edges
// behind (measured — File/Function sources retract inconsistently), while the
// same statements run as separate auto-commit transactions delete every edge.
// Each per-label statement is independently scoped and idempotent, so sequential
// execution is safe (a retry re-runs the same scoped DELETE); the only cost is
// per-label transactions instead of one. Do not "optimize" this back into
// ExecuteGroup without re-proving the grouped path against v1.1.11.
func (w *EdgeWriter) executeCodeCallRetractStatements(ctx context.Context, stmts []Statement) error {
	return w.executeSequentialRetractStatements(ctx, stmts)
}

// executeInheritanceRetractStatements runs the per-child-label inheritance
// retract statements (#5116/#4367) sequentially, each in its own transaction —
// deliberately NOT grouped through ExecuteGroup, for the same NornicDB v1.1.11
// managed-transaction reason documented on executeCodeCallRetractStatements.
// Each statement is independently scoped and idempotent, so sequential execution
// is safe.
func (w *EdgeWriter) executeInheritanceRetractStatements(ctx context.Context, stmts []Statement) error {
	return w.executeSequentialRetractStatements(ctx, stmts)
}

func (w *EdgeWriter) executeSQLRelationshipRetractStatements(ctx context.Context, stmts []Statement) error {
	// NornicDB v1.1.11 acknowledges these label-specific DELETE statements in
	// one managed transaction but applies none of them. Each statement is
	// independently scoped and idempotent, so execute them as separate
	// auto-commit transactions. Do not regroup without re-proving graph truth
	// against the pinned runtime.
	return w.executeSequentialRetractStatements(ctx, stmts)
}

// executeGuardedRationaleDeltaRetracts runs each per-label delta retract behind
// its own existence probe (#5998). probes and retracts are built from the same
// label list in the same order, so probes[i] answers exactly the question
// retracts[i] would delete on.
//
// Why per label rather than one shared probe: each statement matches a
// different target label, so a single probe would answer a narrower question
// than six of the seven deletes it guarded, and a label whose edges exist would
// be skipped on the strength of a different label's emptiness.
//
// Why guard at all: this shape carries the same store-size term as the
// whole-repository retract and is worse per statement — seven statements cost
// about 12s together on a 190,000-relationship store while deleting zero rows
// (ledger:5998-delta-per-label-retract-seeded-rerun), against about 0.29s on an
// empty store (ledger:5998-delta-per-label-retract-empty; same host and image,
// but that empty half is from an earlier session than the seeded figure, which
// the evidence doc records) and 0.31s for the seven probes that guard them
// (ledger:5998-delta-per-label-probe-seeded). It runs on every incremental
// sync, not once per generation.
//
// Concurrency: the per-label statements already run sequentially, each in its
// own transaction, for the NornicDB managed-transaction reason documented on
// executeCodeCallRetractStatements.
//
// What bounds a statement's match set is the repo-qualification of the paths,
// not a repository predicate: these statements bind target.path IN $file_paths
// with no repo_id term, and semanticQualifyDeltaPath (rationale/delta_scope.go)
// is what makes those paths repository-specific. The code-call, inheritance,
// and SQL delta retracts are shaped the same way.
//
// Edge writes carrying the retract_via_refresh marker are fenced behind the
// refresh that owns this retract, by same-batch ordering and by the durable
// completed-intents gate, so a marked writer cannot insert a matching EXPLAINS
// edge between a label's probe and that label's delete. Unmarked legacy rows
// bypass that fence (shared_projection_worker_refresh_fence.go), so state the
// residual honestly rather than claiming no writer can race: an unmarked row
// landing inside the probe-to-delete window is no worse under the guard than
// without it. Skip-then-write leaves a correct current-generation edge, and
// write-then-delete is the pre-existing #2910 behavior the guard does not
// change.
//
// Fail-safe direction matches the whole-repository guard: no ProbeExecutor, or
// a probe error, runs that label's DELETE unconditionally. Only a definitive
// zero skips. A redundant delete is slow; a skipped one leaves stale edges.
func (w *EdgeWriter) executeGuardedRationaleDeltaRetracts(
	ctx context.Context,
	probes []Statement,
	retracts []Statement,
	repoIDs []string,
) error {
	if len(probes) != len(retracts) {
		return fmt.Errorf(
			"rationale delta retract guard: %d probes for %d retracts; they are built from one label list and must pair",
			len(probes), len(retracts),
		)
	}
	pe, canProbe := w.executor.(ProbeExecutor)
	for i, retract := range retracts {
		if !canProbe {
			w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeUnsupported, repoIDs, rationaleDeltaProbeScope, 0, nil)
			if err := w.executor.Execute(ctx, retract); err != nil {
				return WrapRetryableNeo4jError(err)
			}
			continue
		}
		start := time.Now()
		found, err := pe.ExecuteProbe(ctx, probes[i])
		duration := time.Since(start).Seconds()
		switch {
		case err != nil:
			w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeProbeError, repoIDs, rationaleDeltaProbeScope, duration, err)
		case !found:
			w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeSkipped, repoIDs, rationaleDeltaProbeScope, duration, nil)
			continue
		default:
			w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeDeleted, repoIDs, rationaleDeltaProbeScope, duration, nil)
		}
		if err := w.executor.Execute(ctx, retract); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

// executeSequentialRetractStatements runs independently scoped, idempotent
// retract statements in separate auto-commit transactions.
func (w *EdgeWriter) executeSequentialRetractStatements(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

// executeDocumentationRetractStatements runs the documentation delta retract
// statements (section-uid and document-id scoped) sequentially, each in its
// own transaction — deliberately NOT grouped through ExecuteGroup, for the
// same NornicDB v1.1.11 managed-transaction reason documented on
// executeCodeCallRetractStatements. Each statement is independently scoped and
// idempotent, so sequential execution is safe.
func (w *EdgeWriter) executeDocumentationRetractStatements(ctx context.Context, stmts []Statement) error {
	return w.executeSequentialRetractStatements(ctx, stmts)
}

func buildRetractStatement(
	domain string,
	repoIDs []string,
	evidenceSource string,
) (Statement, error) {
	switch domain {
	case reducer.DomainWorkloadDependency:
		return BuildRetractWorkloadDependencyEdges(repoIDs, evidenceSource), nil
	// DomainCodeCalls is handled before this shared repo-id path in RetractEdges
	// because its retract fans out to one per-source-label statement (#5116) and
	// must never reach this single-statement builder.
	// DomainInheritanceEdges is handled before this shared repo-id path in
	// RetractEdges because its retract fans out to one per-child-label statement
	// (#5116/#4367) and must never reach this single-statement builder.
	// DomainDocumentationEdges is handled before this shared repo-id path in
	// RetractEdges because documentation retracts anchor on section.scope_id, not
	// a repository id. It must never reach this repo-id-bound builder.
	// DomainRationaleEdges is handled before this shared repo-id path in
	// RetractEdges (retractRationaleEdgesWithProbe, #5998) because its retract
	// runs a probe-then-delete guard, not a bare Execute of the builder's
	// statement. It must never reach this single-statement builder.
	// DomainSQLRelationships is handled before this shared repo-id path in
	// RetractEdges because its retract fans out to one per-source-label
	// statement run sequentially (the SQL sibling of #5116) and must never
	// reach this single-statement builder; the old unlabeled-scan fallback
	// silently under-deleted on NornicDB v1.1.11.
	case reducer.DomainShellExec:
		return BuildRetractShellExecEdges(repoIDs, evidenceSource), nil
	case reducer.DomainDeployableUnitEdges:
		return BuildRetractDeployableUnitCorrelationEdges(repoIDs, evidenceSource), nil
	case reducer.DomainHandlesRoute:
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractHandlesRouteEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":        repoIDs,
				"evidence_source": evidenceSource,
			},
		}, nil
	case reducer.DomainRunsIn:
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractRunsInEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":        repoIDs,
				"evidence_source": evidenceSource,
			},
		}, nil
	case reducer.DomainInvokesCloudAction:
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractInvokesCloudActionEdgesCypher,
			Parameters: map[string]any{
				"repo_ids":        repoIDs,
				"evidence_source": evidenceSource,
			},
		}, nil
	case reducer.DomainCodeownersOwnershipEdges:
		return BuildRetractCodeownersOwnershipEdges(repoIDs, evidenceSource), nil
	// DomainSubmodulePinEdges never reaches the file-path-scoped
	// collectDeltaFilePaths branch above: buildSubmodulePinRetractRows
	// (submodule_pin_delta_scope.go) only ever emits Payload-less
	// whole-repository retract rows (or skips a repo entirely when its delta
	// did not touch ".gitmodules"), so every retract row lands here with the
	// single repo-anchored whole-repository statement below.
	case reducer.DomainSubmodulePinEdges:
		return BuildRetractSubmodulePinEdges(repoIDs, evidenceSource), nil
	default:
		return Statement{}, fmt.Errorf("unsupported domain for retract: %q", domain)
	}
}

// retractFencedRepoWideDomain handles the domains whose retract is owned by a
// per-repo refresh intent behind the #2898 fence: the narrowed half of
// wholeScopeRetractDomains (edge_writer_retract_scope.go), which the guard below
// asks rather than re-listing (#6276). It reports handled=false for every other
// domain, so RetractEdges falls through to the repo-keyed group.
//
// They narrow to collectWholeScopeRefreshRepoIDs rather than the batch-wide
// collectRepoIDs, through narrowedWholeScopeRepoIDs (#6166): a whole-repository
// DELETE bound to an unmarked legacy per-edge row erases that repository's edges
// across every file while only this batch's rows get rewritten. That table's doc
// carries the full mechanism and the per-domain reachability argument.
//
// Each also runs the whole-scope retract AFTER its delta statements, not
// instead of them (#5998 review F6, extended to the three siblings by #6216). A
// ProcessPartitionOnce batch is selected by partition ID, not by partition key,
// so it routinely carries refresh rows for many repositories: one on a delta
// generation, another on a full generation. collectDeltaFilePaths only inspects
// the delta-flagged rows, so returning after them leaves the full-generation
// sibling with no retract at all -- the refresh intent owns that retract,
// nothing else issues it, and nothing errors, so its stale edges survive
// silently. An empty list there is the ordinary all-delta batch and is
// deliberately not logged as a skip; on the non-delta path below it is
// anomalous, and is.
func (w *EdgeWriter) retractFencedRepoWideDomain(
	ctx context.Context,
	domain string,
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) (bool, error) {
	if !isWholeScopeNarrowedDomain(domain) {
		return false, nil
	}

	if domain == reducer.DomainInheritanceEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return true, err
		}
		if hasDeltaScope {
			stmts := BuildRetractInheritanceEdgeStatementsByFilePath(filePaths, evidenceSource)
			if err := w.executeInheritanceRetractStatements(ctx, stmts); err != nil {
				return true, err
			}
			wholeScopeRepoIDs := collectWholeScopeRefreshRepoIDs(rows)
			if len(wholeScopeRepoIDs) == 0 {
				return true, nil
			}
			return true, w.executeInheritanceRetractStatements(
				ctx,
				BuildRetractInheritanceEdgeStatements(wholeScopeRepoIDs, evidenceSource),
			)
		}
		wholeScopeRepoIDs, skip, err := w.narrowedWholeScopeRepoIDs(domain, rows, evidenceSource)
		if err != nil {
			return true, err
		}
		if skip {
			return true, nil
		}
		stmts := BuildRetractInheritanceEdgeStatements(wholeScopeRepoIDs, evidenceSource)
		return true, w.executeInheritanceRetractStatements(ctx, stmts)
	}
	if domain == reducer.DomainRationaleEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return true, err
		}
		if hasDeltaScope {
			// Per-target-label statements run sequentially (#5116 sibling): a
			// target-label disjunction matches zero rows on NornicDB v1.1.11.
			// The canonical source also retracts the one bounded legacy source
			// in each statement; custom sources remain exact-source retracts.
			stmts := BuildRetractRationaleEdgeStatementsByFilePath(filePaths, evidenceSource)
			probes := BuildProbeRationaleEdgeStatementsByFilePath(filePaths, evidenceSource)
			if err := w.executeGuardedRationaleDeltaRetracts(ctx, probes, stmts, collectDeltaProjectionRepoIDs(rows)); err != nil {
				return true, err
			}
			wholeScopeRepoIDs := collectWholeScopeRefreshRepoIDs(rows)
			if len(wholeScopeRepoIDs) == 0 {
				return true, nil
			}
			return true, w.retractRationaleEdgesWithProbe(ctx, wholeScopeRepoIDs, evidenceSource)
		}
		wholeScopeRepoIDs, skip, err := w.narrowedWholeScopeRepoIDs(domain, rows, evidenceSource)
		if err != nil {
			return true, err
		}
		if skip {
			return true, nil
		}
		return true, w.retractRationaleEdgesWithProbe(ctx, wholeScopeRepoIDs, evidenceSource)
	}

	if domain == reducer.DomainSQLRelationships {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return true, err
		}
		if hasDeltaScope {
			stmts := BuildRetractSQLRelationshipEdgeStatementsByFilePath(filePaths, evidenceSource)
			if err := w.executeSQLRelationshipRetractStatements(ctx, stmts); err != nil {
				return true, err
			}
			wholeScopeRepoIDs := collectWholeScopeRefreshRepoIDs(rows)
			if len(wholeScopeRepoIDs) == 0 {
				return true, nil
			}
			return true, w.executeSQLRelationshipRetractStatements(
				ctx,
				BuildRetractSQLRelationshipEdgeStatements(wholeScopeRepoIDs, evidenceSource),
			)
		}
		wholeScopeRepoIDs, skip, err := w.narrowedWholeScopeRepoIDs(domain, rows, evidenceSource)
		if err != nil {
			return true, err
		}
		if skip {
			return true, nil
		}
		stmts := BuildRetractSQLRelationshipEdgeStatements(wholeScopeRepoIDs, evidenceSource)
		return true, w.executeSQLRelationshipRetractStatements(ctx, stmts)
	}
	if domain == reducer.DomainShellExec {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return true, err
		}
		if hasDeltaScope {
			if err := w.retractShellExecEdgesByFilePath(ctx, filePaths, evidenceSource); err != nil {
				return true, err
			}
			wholeScopeRepoIDs := collectWholeScopeRefreshRepoIDs(rows)
			if len(wholeScopeRepoIDs) == 0 {
				return true, nil
			}
			return true, w.retractShellExecEdges(ctx, wholeScopeRepoIDs, evidenceSource)
		}
		wholeScopeRepoIDs, skip, err := w.narrowedWholeScopeRepoIDs(domain, rows, evidenceSource)
		if err != nil {
			return true, err
		}
		if skip {
			return true, nil
		}
		return true, w.retractShellExecEdges(ctx, wholeScopeRepoIDs, evidenceSource)
	}
	// Reachable only for a domain registered in the narrowed half of
	// wholeScopeRetractDomains with no branch above; falling through to the
	// batch-wide repo-keyed group is the over-delete the narrowing prevents.
	return true, fmt.Errorf(
		"domain %q is narrowed in wholeScopeRetractDomains (edge_writer_retract_scope.go) but retractFencedRepoWideDomain has no branch for it",
		domain,
	)
}
