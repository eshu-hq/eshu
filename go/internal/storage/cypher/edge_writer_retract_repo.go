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
	return buildRepoDependencySplitRetractStatements(repoIDs, evidenceSource)
}

func buildRepoDependencyDiagnosticRetractStatements(repoIDs []string, evidenceSource string) []repoDependencyRetractStatement {
	return buildRepoDependencySplitRetractStatements(repoIDs, evidenceSource)
}

func buildRepoDependencySplitRetractStatements(repoIDs []string, evidenceSource string) []repoDependencyRetractStatement {
	return []repoDependencyRetractStatement{
		{
			role: "repository_relationship_edges",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    repoDependencyRelationshipRetractCypher(repoIDs),
				Parameters: repoDependencyRetractParameters(
					repoIDs,
					evidenceSource,
					repoDependencyRetractSummary("repository_relationship_edges", repoDependencyRelationshipEdgeTypes),
				),
			},
		},
		{
			role: "runs_on_relationships",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    repoDependencyRunsOnRetractCypher(repoIDs),
				Parameters: repoDependencyRetractParameters(
					repoIDs,
					evidenceSource,
					"role=runs_on_relationships relationships=RUNS_ON",
				),
			},
		},
		{
			role: "evidence_artifacts",
			stmt: Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    repoDependencyEvidenceArtifactRetractCypher(repoIDs),
				Parameters: repoDependencyRetractParameters(
					repoIDs,
					evidenceSource,
					"role=evidence_artifacts relationships=HAS_DEPLOYMENT_EVIDENCE",
				),
			},
		},
	}
}

func repoDependencyRelationshipRetractCypher(repoIDs []string) string {
	if len(repoIDs) == 1 {
		return retractSingleRepoRelationshipEdgesCypher
	}
	return retractRepoRelationshipEdgesCypher
}

func repoDependencyRunsOnRetractCypher(repoIDs []string) string {
	if len(repoIDs) == 1 {
		return retractSingleRepoRunsOnEdgesCypher
	}
	return retractRepoRunsOnEdgesCypher
}

func repoDependencyEvidenceArtifactRetractCypher(repoIDs []string) string {
	if len(repoIDs) == 1 {
		return retractSingleRepoEvidenceArtifactsCypher
	}
	return retractRepoEvidenceArtifactsCypher
}

func repoDependencyRetractParameters(repoIDs []string, evidenceSource string, summary string) map[string]any {
	params := map[string]any{
		"evidence_source":           evidenceSource,
		StatementMetadataSummaryKey: summary,
	}
	if len(repoIDs) == 1 {
		params["repo_id"] = repoIDs[0]
		return params
	}
	params["repo_ids"] = repoIDs
	return params
}

// executeRepoDependencyRetractStatements runs the three repo-dependency
// retract statements sequentially, each in its own transaction — deliberately
// NOT grouped through ExecuteGroup, for the same NornicDB v1.1.11
// managed-transaction reason documented on executeCodeCallRetractStatements
// (measured here too: the grouped path left the first statement's typed
// relationship edges undeleted). Each statement is independently scoped and
// idempotent, so sequential execution is safe.
func (w *EdgeWriter) executeRepoDependencyRetractStatements(ctx context.Context, repoIDs []string, evidenceSource string) error {
	items := buildRepoDependencyRetractStatements(repoIDs, evidenceSource)
	if w.RepoDependencyRetractStatementTiming {
		items = buildRepoDependencyDiagnosticRetractStatements(repoIDs, evidenceSource)
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
