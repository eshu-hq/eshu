// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EdgeWriter implements reducer.SharedProjectionEdgeWriter by dispatching
// domain-specific canonical Cypher statements through an Executor.
// Writes are batched using UNWIND for efficiency.
type EdgeWriter struct {
	executor Executor
	// Reader runs bounded read queries for retract paths that must compute a
	// Go-side anti-join before deleting instead of relying on a Cypher
	// relationship-existence predicate (`NOT (n)--()`, `(n)--()`, or
	// `COUNT { (n)--() } = 0`), all of which are mis-evaluated on the pinned
	// NornicDB backends -- see docs/public/reference/nornicdb-pitfalls.md
	// ("Every Relationship-Existence Predicate Is Mis-Evaluated"). Required
	// for shell-exec retracts (orphan ShellCommand cleanup); nil for a writer
	// that never routes DomainShellExec retracts.
	Reader                        OrphanSweepReader
	BatchSize                     int
	CodeCallBatchSize             int
	CodeCallGroupBatchSize        int
	InheritanceGroupBatchSize     int
	SQLRelationshipGroupBatchSize int
	// SQLRelationshipSequentialWrites routes SQL relationship statements through
	// auto-commit Execute even when the executor supports managed groups. The
	// pinned NornicDB backend acknowledges these UNWIND/MATCH/MERGE statements in
	// a managed transaction without persisting their edges (#5410).
	SQLRelationshipSequentialWrites bool
	// RepoDependencyRetractStatementTiming is retained for environment
	// compatibility (ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING) but no
	// longer changes behavior: repo_dependency retracts run their source-capable
	// statements sequentially with per-statement timing logs, because grouped
	// DELETEs under-apply on NornicDB v1.1.11 (#4367). Code-import evidence runs
	// two roles because that producer cannot emit RUNS_ON; other sources run all
	// three roles.
	RepoDependencyRetractStatementTiming bool
	Instruments                          *telemetry.Instruments
	Logger                               *slog.Logger
}

// NewEdgeWriter returns an EdgeWriter backed by the given Executor.
// A batchSize of 0 or less uses DefaultBatchSize (500).
func NewEdgeWriter(executor Executor, batchSize int) *EdgeWriter {
	return &EdgeWriter{executor: executor, BatchSize: batchSize}
}

func (w *EdgeWriter) batchSize() int {
	if w.BatchSize <= 0 {
		return DefaultBatchSize
	}
	return w.BatchSize
}

func (w *EdgeWriter) batchSizeForDomain(domain string) int {
	if domain == reducer.DomainCodeCalls && w.CodeCallBatchSize > 0 {
		return w.CodeCallBatchSize
	}
	return w.batchSize()
}

func (w *EdgeWriter) codeCallGroupBatchSize() int {
	if w.CodeCallGroupBatchSize <= 0 {
		return 0
	}
	return w.CodeCallGroupBatchSize
}

func (w *EdgeWriter) groupBatchSizeForDomain(domain string) int {
	switch domain {
	case reducer.DomainCodeCalls:
		return w.codeCallGroupBatchSize()
	case reducer.DomainInheritanceEdges:
		if w.InheritanceGroupBatchSize <= 0 {
			return 0
		}
		return w.InheritanceGroupBatchSize
	case reducer.DomainSQLRelationships:
		if w.SQLRelationshipGroupBatchSize <= 0 {
			return 0
		}
		return w.SQLRelationshipGroupBatchSize
	case reducer.DomainShellExec:
		if w.SQLRelationshipGroupBatchSize <= 0 {
			return 0
		}
		return w.SQLRelationshipGroupBatchSize
	default:
		return 0
	}
}

// WriteEdges writes canonical domain edges for the given rows using batched
// UNWIND statements. Rows with empty required MATCH fields are skipped to
// avoid silent failures in the batch.
//
// When the executor implements GroupExecutor, all batches are dispatched in a
// single atomic transaction.
func (w *EdgeWriter) WriteEdges(
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
	if _, err := batchCypherForDomain(domain); err != nil {
		return err
	}
	if domain == reducer.DomainRepoDependency {
		if err := validateRepoDependencySourceRows(rows, evidenceSource); err != nil {
			return err
		}
	}

	routedRows := make(map[string][]map[string]any)
	routeOrder := make([]string, 0, 2)
	artifactRows := make(map[string][]map[string]any)
	artifactRouteOrder := make([]string, 0, 2)
	for _, row := range rows {
		cypher, rowMap, ok := buildRowMap(domain, row, evidenceSource)
		if !ok {
			continue
		}
		if _, seen := routedRows[cypher]; !seen {
			routeOrder = append(routeOrder, cypher)
		}
		routedRows[cypher] = append(routedRows[cypher], rowMap)
		if domain == reducer.DomainRepoDependency {
			for _, artifactRow := range repoEvidenceArtifactRowsFromIntent(row, evidenceSource) {
				artifactCypher := batchCanonicalRepoEvidenceArtifactUpsertCypher
				if payloadString(artifactRow, "environment") != "" {
					artifactCypher = batchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher
				}
				if _, seen := artifactRows[artifactCypher]; !seen {
					artifactRouteOrder = append(artifactRouteOrder, artifactCypher)
				}
				artifactRows[artifactCypher] = append(artifactRows[artifactCypher], artifactRow)
			}
		}
	}

	if len(routedRows) == 0 {
		return nil
	}
	writtenRows := 0
	for _, cypher := range routeOrder {
		writtenRows += len(routedRows[cypher])
	}

	// Collect all batches as statements.
	var stmts []Statement
	bs := w.batchSizeForDomain(domain)
	for _, cypher := range routeOrder {
		routeStatements := buildBatchedStatements(cypher, routedRows[cypher], bs)
		annotateEdgeStatementSummaries(domain, cypher, routeStatements)
		stmts = append(stmts, routeStatements...)
	}
	for _, cypher := range artifactRouteOrder {
		routeStatements := buildBatchedStatements(cypher, artifactRows[cypher], bs)
		annotateEdgeStatementSummaries(domain, cypher, routeStatements)
		stmts = append(stmts, routeStatements...)
	}

	// Prefer atomic grouped execution except where the backend requires the SQL
	// relationship write path to use auto-commit for accurate graph truth.
	sequentialSQL := domain == reducer.DomainSQLRelationships && w.SQLRelationshipSequentialWrites
	if ge, ok := w.executor.(GroupExecutor); ok && !sequentialSQL {
		if groupSize := w.groupBatchSizeForDomain(domain); groupSize > 0 {
			for i := 0; i < len(stmts); i += groupSize {
				end := i + groupSize
				if end > len(stmts) {
					end = len(stmts)
				}
				start := time.Now()
				if err := ge.ExecuteGroup(ctx, stmts[i:end]); err != nil {
					return WrapRetryableNeo4jError(err)
				}
				duration := time.Since(start).Seconds()
				w.recordGroupedWrite(ctx, domain, duration, stmts[i:end])
				w.logSharedEdgeWrite(domain, evidenceSource, "group", len(rows), writtenRows, len(routeOrder), bs, groupSize, duration, stmts[i:end])
				if domain == reducer.DomainCodeCalls {
					w.recordCodeCallBatch(ctx, duration)
				}
			}
			return nil
		}
		start := time.Now()
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		duration := time.Since(start).Seconds()
		w.recordGroupedWrite(ctx, domain, duration, stmts)
		w.logSharedEdgeWrite(domain, evidenceSource, "group", len(rows), writtenRows, len(routeOrder), bs, 0, duration, stmts)
		return nil
	}

	for _, stmt := range stmts {
		start := time.Now()
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		duration := time.Since(start).Seconds()
		w.logSharedEdgeWrite(domain, evidenceSource, "single", len(rows), writtenRows, len(routeOrder), bs, 0, duration, []Statement{stmt})
		if domain == reducer.DomainCodeCalls {
			w.recordCodeCallBatch(ctx, duration)
		}
	}
	return nil
}

// batchCypherForDomain returns the batched UNWIND Cypher template for the
// given shared projection domain. WriteEdges uses it only as a
// domain-recognition gate (its returned template is discarded there; each row's
// Cypher comes from buildRowMap). The DomainSQLRelationships case returns an
// empty template on purpose — that domain is dispatched per relationship type,
// with no single-template batch (see the case comment).
func batchCypherForDomain(domain string) (string, error) {
	switch domain {
	case reducer.DomainRepoDependency:
		return batchCanonicalRepoDependencyUpsertCypher, nil
	case reducer.DomainWorkloadDependency:
		return batchCanonicalWorkloadDependencyUpsertCypher, nil
	case reducer.DomainCodeCalls:
		return batchCanonicalCodeCallUpsertCypher, nil
	case reducer.DomainInheritanceEdges:
		return batchCanonicalInheritanceEdgeUpsertCypher, nil
	case reducer.DomainDocumentationEdges:
		return batchCanonicalDocumentationEntityEdgeCypher, nil
	case reducer.DomainRationaleEdges:
		return batchCanonicalRationaleExplainsEdgeCypher, nil
	case reducer.DomainSQLRelationships:
		// SQL relationship edges have no single-template batch: each row is
		// dispatched per relationship type by buildSQLRelationshipRowMap — a
		// label-scoped MERGE for SqlView/SqlFunction/SqlTable/SqlMigration/... endpoints
		// (READS_FROM, WRITES_TO, REFERENCES_TABLE, INDEXES, MIGRATES, ...) or the
		// QUERIES_TABLE/HAS_COLUMN/TRIGGERS/EXECUTES fallback templates for
		// mixed-label endpoints. This case exists only to satisfy WriteEdges'
		// domain-recognition gate. It deliberately returns an empty template
		// rather than a single stale rel-type Cypher (it used to hardcode
		// REFERENCES_TABLE, which #5345 renamed to READS_FROM): a future caller
		// that misused this return value would then write nothing — an obvious,
		// loud failure — instead of silently MERGE-ing every SQL edge under one
		// wrong relationship type.
		return "", nil
	case reducer.DomainShellExec:
		return batchCanonicalShellExecUpsertCypher, nil
	case reducer.DomainDeployableUnitEdges:
		return batchCanonicalDeployableUnitCorrelationUpsertCypher, nil
	case reducer.DomainHandlesRoute:
		return batchCanonicalHandlesRouteEdgeUpsertCypher, nil
	case reducer.DomainRunsIn:
		return batchCanonicalRunsInEdgeUpsertCypher, nil
	case reducer.DomainInvokesCloudAction:
		return batchCanonicalInvokesCloudActionUpsertCypher, nil
	case reducer.DomainCodeownersOwnershipEdges:
		return batchCanonicalCodeownersOwnershipEdgeCypher, nil
	case reducer.DomainSubmodulePinEdges:
		return batchCanonicalSubmodulePinEdgeCypher, nil
	default:
		return "", fmt.Errorf("unsupported domain for write: %q", domain)
	}
}

// repoDependencyRowMapCapacity and repoRelationshipRowMapCapacity pre-size
// the DEPENDS_ON and typed-relationship row maps in buildRowMap to their
// worst-case key count (#5441 perf follow-up). A map literal is sized from
// its own element count only, so every key appended afterward can force a
// bucket-array reallocation once the map crosses Go's load-factor threshold;
// pre-sizing avoids it. Theory proved and measured before implementing —
// see "Map Bucket-Growth Pre-Sizing" in
// docs/internal/evidence/5441-edge-node-properties.md for the throwaway-shim
// numbers and the recovered before/after/pre-sized benchmark table.
const (
	repoDependencyRowMapCapacity   = 14 // 3 base + evidence_type + source_tool + 9 copyRepoRelationshipMetadata keys.
	repoRelationshipRowMapCapacity = 15 // 4 base + evidence_type + source_tool + 9 copyRepoRelationshipMetadata keys.
)

// buildRowMap converts a SharedProjectionIntentRow into a flat parameter map
// suitable for UNWIND batching. Returns false if required MATCH fields for the
// domain are empty, indicating the row should be skipped.
func buildRowMap(
	domain string,
	row reducer.SharedProjectionIntentRow,
	evidenceSource string,
) (string, map[string]any, bool) {
	switch domain {
	case reducer.DomainRepoDependency:
		repoID := payloadString(row.Payload, "repo_id")
		targetRepoID := payloadString(row.Payload, "target_repo_id")
		relationshipType := payloadString(row.Payload, "relationship_type")
		if repoID == "" {
			return "", nil, false
		}
		if relationshipType == string(edgetype.RunsOn) {
			platformID := payloadString(row.Payload, "platform_id")
			if platformID == "" {
				return "", nil, false
			}
			rowMap := map[string]any{
				"repo_id":         repoID,
				"platform_id":     platformID,
				"evidence_source": evidenceSource,
			}
			if evidenceType := payloadString(row.Payload, "evidence_type"); evidenceType != "" {
				rowMap["evidence_type"] = evidenceType
			}
			if sourceTool := payloadString(row.Payload, "source_tool"); sourceTool != "" {
				rowMap["source_tool"] = sourceTool
			}
			return batchCanonicalRunsOnUpsertCypher, rowMap, true
		}
		if targetRepoID == "" {
			return "", nil, false
		}
		if relationshipType == "" || relationshipType == string(edgetype.DependsOn) {
			rowMap := make(map[string]any, repoDependencyRowMapCapacity)
			rowMap["repo_id"] = repoID
			rowMap["target_repo_id"] = targetRepoID
			rowMap["evidence_source"] = evidenceSource
			if evidenceType := payloadString(row.Payload, "evidence_type"); evidenceType != "" {
				rowMap["evidence_type"] = evidenceType
			}
			if sourceTool := payloadString(row.Payload, "source_tool"); sourceTool != "" {
				rowMap["source_tool"] = sourceTool
			}
			copyRepoRelationshipMetadata(rowMap, row.Payload, row.GenerationID)
			return batchCanonicalRepoDependencyUpsertCypher, rowMap, true
		}
		rowMap := make(map[string]any, repoRelationshipRowMapCapacity)
		rowMap["repo_id"] = repoID
		rowMap["target_repo_id"] = targetRepoID
		rowMap["relationship_type"] = relationshipType
		rowMap["evidence_source"] = evidenceSource
		if evidenceType := payloadString(row.Payload, "evidence_type"); evidenceType != "" {
			rowMap["evidence_type"] = evidenceType
		}
		if sourceTool := payloadString(row.Payload, "source_tool"); sourceTool != "" {
			rowMap["source_tool"] = sourceTool
		}
		copyRepoRelationshipMetadata(rowMap, row.Payload, row.GenerationID)
		cypher, ok := batchCanonicalTypedRepoRelationshipUpsertCypher(relationshipType)
		if !ok {
			return "", nil, false
		}
		return cypher, rowMap, true

	case reducer.DomainWorkloadDependency:
		workloadID := payloadString(row.Payload, "workload_id")
		targetWorkloadID := payloadString(row.Payload, "target_workload_id")
		if workloadID == "" || targetWorkloadID == "" {
			return "", nil, false
		}
		return batchCanonicalWorkloadDependencyUpsertCypher, map[string]any{
			"workload_id":        workloadID,
			"target_workload_id": targetWorkloadID,
			"evidence_source":    evidenceSource,
		}, true

	case reducer.DomainCodeCalls:
		return buildCodeCallRowMap(row.Payload, evidenceSource)

	case reducer.DomainInheritanceEdges:
		return buildInheritanceRowMap(row.Payload, evidenceSource)

	case reducer.DomainDocumentationEdges:
		return buildDocumentationRowMap(row.Payload, evidenceSource)

	case reducer.DomainRationaleEdges:
		return buildRationaleRowMap(row.Payload, evidenceSource)

	case reducer.DomainSQLRelationships:
		return buildSQLRelationshipRowMap(row.Payload, evidenceSource)

	case reducer.DomainShellExec:
		return buildShellExecRowMap(row.Payload, evidenceSource)

	case reducer.DomainDeployableUnitEdges:
		return buildDeployableUnitCorrelationRowMap(row.Payload, evidenceSource)

	case reducer.DomainHandlesRoute:
		return buildHandlesRouteRowMap(row.Payload, evidenceSource)

	case reducer.DomainRunsIn:
		return buildRunsInRowMap(row.Payload, evidenceSource)

	case reducer.DomainInvokesCloudAction:
		return buildInvokesCloudActionRowMap(row.Payload, evidenceSource)

	case reducer.DomainCodeownersOwnershipEdges:
		return buildCodeownersOwnershipRowMap(row.Payload, evidenceSource)

	case reducer.DomainSubmodulePinEdges:
		return buildSubmodulePinRowMap(row.Payload, evidenceSource)

	default:
		return "", nil, false
	}
}

// buildHandlesRouteRowMap and buildDeployableUnitCorrelationRowMap live in
// edge_writer_rowmaps.go (split out to keep this file under the 500-line
// cap when the submodule_pin domain case was added, issue #5420 Phase 3).
