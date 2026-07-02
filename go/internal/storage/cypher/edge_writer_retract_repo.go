// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type repoDependencyRetractStatement struct {
	role string
	stmt Statement
}

func repoDependencyRetractSummary(role string, relationships string) string {
	return "role=" + role + " relationships=" + relationships
}

func buildRepoDependencyRetractStatements(repoIDs []string, evidenceSource string) []repoDependencyRetractStatement {
	return []repoDependencyRetractStatement{
		{
			role: "repository_relationships",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoRelationshipAndRunsOnEdgesCypher,
				Parameters: map[string]any{
					"repo_ids":                  repoIDs,
					"evidence_source":           evidenceSource,
					StatementMetadataSummaryKey: repoDependencyRetractSummary("repository_relationships", repoDependencyRelationshipEdgeTypes+"|RUNS_ON"),
				},
			},
		},
		{
			role: "evidence_artifacts",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoEvidenceArtifactsCypher,
				Parameters: map[string]any{
					"repo_ids":                  repoIDs,
					"evidence_source":           evidenceSource,
					StatementMetadataSummaryKey: "role=evidence_artifacts relationships=HAS_DEPLOYMENT_EVIDENCE",
				},
			},
		},
	}
}

func buildRepoDependencyDiagnosticRetractStatements(repoIDs []string, evidenceSource string) []repoDependencyRetractStatement {
	return []repoDependencyRetractStatement{
		{
			role: "repository_relationship_edges",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoRelationshipEdgesCypher,
				Parameters: map[string]any{
					"repo_ids":                  repoIDs,
					"evidence_source":           evidenceSource,
					StatementMetadataSummaryKey: repoDependencyRetractSummary("repository_relationship_edges", repoDependencyRelationshipEdgeTypes),
				},
			},
		},
		{
			role: "runs_on_relationships",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoRunsOnEdgesCypher,
				Parameters: map[string]any{
					"repo_ids":                  repoIDs,
					"evidence_source":           evidenceSource,
					StatementMetadataSummaryKey: "role=runs_on_relationships relationships=RUNS_ON",
				},
			},
		},
		{
			role: "evidence_artifacts",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    retractRepoEvidenceArtifactsCypher,
				Parameters: map[string]any{
					"repo_ids":                  repoIDs,
					"evidence_source":           evidenceSource,
					StatementMetadataSummaryKey: "role=evidence_artifacts relationships=HAS_DEPLOYMENT_EVIDENCE",
				},
			},
		},
	}
}

func (w *EdgeWriter) executeRepoDependencyRetractStatements(ctx context.Context, repoIDs []string, evidenceSource string) error {
	items := buildRepoDependencyRetractStatements(repoIDs, evidenceSource)
	if w.RepoDependencyRetractStatementTiming {
		items = buildRepoDependencyDiagnosticRetractStatements(repoIDs, evidenceSource)
	}
	if ge, ok := w.executor.(GroupExecutor); ok && !w.RepoDependencyRetractStatementTiming {
		executableStmts := make([]Statement, 0, len(items))
		logStmts := make([]Statement, 0, len(items))
		for _, item := range items {
			executableStmts = append(executableStmts, SanitizeStatement(item.stmt))
			logStmts = append(logStmts, item.stmt)
		}
		start := time.Now()
		if err := ge.ExecuteGroup(ctx, executableStmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		w.logSharedEdgeRetractGroup(
			reducer.DomainRepoDependency,
			evidenceSource,
			len(repoIDs),
			time.Since(start).Seconds(),
			logStmts,
		)
		return nil
	}

	return w.executeRepoDependencyRetractStatementsSequential(ctx, items, repoIDs, evidenceSource)
}

func (w *EdgeWriter) executeRepoDependencyRetractStatementsSequential(
	ctx context.Context,
	items []repoDependencyRetractStatement,
	repoIDs []string,
	evidenceSource string,
) error {
	for _, item := range items {
		start := time.Now()
		if err := w.executor.Execute(ctx, SanitizeStatement(item.stmt)); err != nil {
			return WrapRetryableNeo4jError(fmt.Errorf("repo dependency retract %s: %w", item.role, err))
		}
		w.logSharedEdgeRetractStatement(
			reducer.DomainRepoDependency,
			evidenceSource,
			item.role,
			len(repoIDs),
			time.Since(start).Seconds(),
			item.stmt,
		)
	}
	return nil
}
