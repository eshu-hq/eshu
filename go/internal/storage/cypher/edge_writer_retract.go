// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

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

	if domain == reducer.DomainInheritanceEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmts := BuildRetractInheritanceEdgeStatementsByFilePath(filePaths, evidenceSource)
			return w.executeInheritanceRetractStatements(ctx, stmts)
		}
	}

	if domain == reducer.DomainRationaleEdges {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			// Per-target-label statements run sequentially (#5116 sibling): a
			// target-label disjunction matches zero rows on NornicDB v1.1.11.
			stmts := BuildRetractRationaleEdgeStatementsByFilePath(filePaths, evidenceSource)
			return w.executeSequentialRetractStatements(ctx, stmts)
		}
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

	repoIDs := collectRepoIDs(rows)
	if domain == reducer.DomainCodeCalls {
		stmts := BuildRetractCodeCallEdgeStatements(repoIDs, evidenceSource)
		return w.executeCodeCallRetractStatements(ctx, stmts)
	}
	if domain == reducer.DomainInheritanceEdges {
		stmts := BuildRetractInheritanceEdgeStatements(repoIDs, evidenceSource)
		return w.executeInheritanceRetractStatements(ctx, stmts)
	}
	if domain == reducer.DomainSQLRelationships {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			stmts := BuildRetractSQLRelationshipEdgeStatementsByFilePath(filePaths, evidenceSource)
			return w.executeSQLRelationshipRetractStatements(ctx, stmts)
		}
		stmts := BuildRetractSQLRelationshipEdgeStatements(repoIDs, evidenceSource)
		return w.executeSQLRelationshipRetractStatements(ctx, stmts)
	}
	if domain == reducer.DomainShellExec {
		filePaths, hasDeltaScope, err := collectDeltaFilePaths(rows)
		if err != nil {
			return err
		}
		if hasDeltaScope {
			return w.retractShellExecEdgesByFilePath(ctx, filePaths, evidenceSource)
		}
		return w.retractShellExecEdges(ctx, repoIDs, evidenceSource)
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
	case reducer.DomainRationaleEdges:
		return BuildRetractRationaleEdges(repoIDs, evidenceSource), nil
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
