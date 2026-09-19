// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type RepoDependencyRetractStatement struct {
	role string
	Stmt sourcecypher.Statement
}

const codeImportRepoDependencyEvidenceSource = "projection/code-imports"

func repoDependencyRetractSummary(role string, relationships string) string {
	return "role=" + role + " relationships=" + relationships
}

func buildRepoDependencyRetractStatements(repoIDs []string, evidenceSource string) []RepoDependencyRetractStatement {
	return BuildRepoDependencySplitRetractStatements(repoIDs, evidenceSource)
}

func buildRepoDependencyDiagnosticRetractStatements(repoIDs []string, evidenceSource string) []RepoDependencyRetractStatement {
	return BuildRepoDependencySplitRetractStatements(repoIDs, evidenceSource)
}

func BuildRepoDependencySplitRetractStatements(repoIDs []string, evidenceSource string) []RepoDependencyRetractStatement {
	items := []RepoDependencyRetractStatement{
		{
			role: "repository_relationship_edges",
			Stmt: sourcecypher.Statement{
				Operation: sourcecypher.OperationCanonicalRetract,
				Cypher:    repoDependencyRelationshipRetractCypher(repoIDs),
				Parameters: repoDependencyRetractParameters(
					repoIDs,
					evidenceSource,
					repoDependencyRetractSummary("repository_relationship_edges", sourcecypher.RepoDependencyRelationshipEdgeTypes),
				),
			},
		},
	}
	if repoDependencySourceSupportsRunsOn(evidenceSource) {
		items = append(items, RepoDependencyRetractStatement{
			role: "runs_on_relationships",
			Stmt: sourcecypher.Statement{
				Operation: sourcecypher.OperationCanonicalRetract,
				Cypher:    repoDependencyRunsOnRetractCypher(repoIDs),
				Parameters: repoDependencyRetractParameters(
					repoIDs,
					evidenceSource,
					"role=runs_on_relationships relationships=RUNS_ON",
				),
			},
		})
	}
	items = append(items, RepoDependencyRetractStatement{
		role: "evidence_artifacts",
		Stmt: sourcecypher.Statement{
			Operation: sourcecypher.OperationCanonicalRetract,
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
		relationshipType := sourcecypher.PayloadString(row.Payload, "relationship_type")
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
		return sourcecypher.RetractSingleRepoRelationshipEdgesCypher
	}
	return sourcecypher.RetractRepoRelationshipEdgesCypher
}

func repoDependencyRunsOnRetractCypher(repoIDs []string) string {
	if len(repoIDs) == 1 {
		return sourcecypher.RetractSingleRepoRunsOnEdgesCypher
	}
	return sourcecypher.RetractRepoRunsOnEdgesCypher
}

func repoDependencyEvidenceArtifactRetractCypher(repoIDs []string) string {
	if len(repoIDs) == 1 {
		return sourcecypher.RetractSingleRepoEvidenceArtifactsCypher
	}
	return sourcecypher.RetractRepoEvidenceArtifactsCypher
}

func repoDependencyRetractParameters(repoIDs []string, evidenceSource string, summary string) map[string]any {
	params := map[string]any{
		"evidence_source":                        evidenceSource,
		sourcecypher.StatementMetadataSummaryKey: summary,
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
	items []RepoDependencyRetractStatement,
	repoIDs []string,
	evidenceSource string,
) error {
	for _, item := range items {
		start := time.Now()
		if err := w.executor.Execute(ctx, sourcecypher.SanitizeStatement(item.Stmt)); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(fmt.Errorf("repo dependency retract %s: %w", item.role, err))
		}
		w.logSharedEdgeRetractStatement(
			reducer.DomainRepoDependency,
			evidenceSource,
			item.role,
			len(repoIDs),
			time.Since(start).Seconds(),
			item.Stmt,
		)
	}
	return nil
}
