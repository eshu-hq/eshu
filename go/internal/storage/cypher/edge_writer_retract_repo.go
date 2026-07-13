// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type repoDependencyRetractStatement struct {
	role string
	stmt Statement
}

const codeImportRepoDependencyEvidenceSource = "projection/code-imports"

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
	items := []repoDependencyRetractStatement{
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
	}
	if repoDependencySourceSupportsRunsOn(evidenceSource) {
		items = append(items, repoDependencyRetractStatement{
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
		})
	}
	items = append(items, repoDependencyRetractStatement{
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
	})
	return items
}

func repoDependencySourceSupportsRunsOn(evidenceSource string) bool {
	return evidenceSource != codeImportRepoDependencyEvidenceSource
}

func validateRepoDependencySourceRows(
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) error {
	if evidenceSource != codeImportRepoDependencyEvidenceSource {
		return nil
	}
	for _, row := range rows {
		relationshipType := payloadString(row.Payload, "relationship_type")
		if relationshipType == "" || relationshipType == string(edgetype.DependsOn) {
			continue
		}
		return fmt.Errorf(
			"repo dependency evidence source %q cannot write relationship type %q",
			evidenceSource,
			relationshipType,
		)
	}
	return nil
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

// executeRepoDependencyRetractStatements runs the source-capable repo-dependency
// retract statements sequentially, each in its own transaction — deliberately
// NOT grouped through ExecuteGroup, for the same NornicDB v1.1.11
// managed-transaction reason documented on executeCodeCallRetractStatements
// (measured here too: the grouped path left the first statement's typed
// relationship edges undeleted). Code-import evidence omits RUNS_ON because
// that producer only emits DEPENDS_ON; all other sources retain the RUNS_ON
// cleanup. Each statement is independently scoped and idempotent, so sequential
// execution is safe.
func (w *EdgeWriter) executeRepoDependencyRetractStatements(ctx context.Context, repoIDs []string, evidenceSource string) error {
	items := buildRepoDependencyRetractStatements(repoIDs, evidenceSource)
	if w.RepoDependencyRetractStatementTiming {
		items = buildRepoDependencyDiagnosticRetractStatements(repoIDs, evidenceSource)
	}
	if !repoDependencySourceSupportsRunsOn(evidenceSource) {
		w.recordSharedEdgeRunsOnRetractOmission(
			ctx,
			reducer.DomainRepoDependency,
			"source_capability",
		)
		w.logSharedEdgeRetractRoleOmitted(
			reducer.DomainRepoDependency,
			evidenceSource,
			"runs_on_relationships",
			len(repoIDs),
			"source_capability",
		)
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
