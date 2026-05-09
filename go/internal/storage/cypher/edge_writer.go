package cypher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EdgeWriter implements reducer.SharedProjectionEdgeWriter by dispatching
// domain-specific canonical Cypher statements through an Executor.
// Writes are batched using UNWIND for efficiency.
type EdgeWriter struct {
	executor                      Executor
	BatchSize                     int
	CodeCallBatchSize             int
	CodeCallGroupBatchSize        int
	InheritanceGroupBatchSize     int
	SQLRelationshipGroupBatchSize int
	Instruments                   *telemetry.Instruments
	Logger                        *slog.Logger
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
		stmts = append(stmts, buildBatchedStatements(cypher, routedRows[cypher], bs)...)
	}
	for _, cypher := range artifactRouteOrder {
		stmts = append(stmts, buildBatchedStatements(cypher, artifactRows[cypher], bs)...)
	}

	// Prefer atomic grouped execution; fall back to sequential.
	if ge, ok := w.executor.(GroupExecutor); ok {
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

func (w *EdgeWriter) recordGroupedWrite(
	ctx context.Context,
	domain string,
	duration float64,
	stmts []Statement,
) {
	if w.Instruments == nil || len(stmts) == 0 {
		return
	}

	attrs := metric.WithAttributes(
		telemetry.AttrDomain(domain),
	)
	w.Instruments.SharedEdgeWriteGroups.Add(ctx, 1, attrs)
	w.Instruments.SharedEdgeWriteGroupDuration.Record(ctx, duration, attrs)
	w.Instruments.SharedEdgeWriteGroupStatementCount.Record(ctx, int64(len(stmts)), attrs)
}

func (w *EdgeWriter) recordCodeCallBatch(ctx context.Context, duration float64) {
	if w.Instruments == nil {
		return
	}

	attrs := metric.WithAttributes(
		telemetry.AttrDomain(reducer.DomainCodeCalls),
	)
	w.Instruments.CodeCallEdgeBatches.Add(ctx, 1, attrs)
	w.Instruments.CodeCallEdgeDuration.Record(ctx, duration, attrs)
}

// batchCypherForDomain returns the batched UNWIND Cypher template for the
// given shared projection domain.
func batchCypherForDomain(domain string) (string, error) {
	switch domain {
	case reducer.DomainPlatformInfra:
		return batchCanonicalInfrastructurePlatformUpsertCypher, nil
	case reducer.DomainRepoDependency:
		return batchCanonicalRepoDependencyUpsertCypher, nil
	case reducer.DomainWorkloadDependency:
		return batchCanonicalWorkloadDependencyUpsertCypher, nil
	case reducer.DomainCodeCalls:
		return batchCanonicalCodeCallUpsertCypher, nil
	case reducer.DomainInheritanceEdges:
		return batchCanonicalInheritanceEdgeUpsertCypher, nil
	case reducer.DomainSQLRelationships:
		return batchCanonicalSQLRelationshipUpsertCypher, nil
	default:
		return "", fmt.Errorf("unsupported domain for write: %q", domain)
	}
}

// buildRowMap converts a SharedProjectionIntentRow into a flat parameter map
// suitable for UNWIND batching. Returns false if required MATCH fields for the
// domain are empty, indicating the row should be skipped.
func buildRowMap(
	domain string,
	row reducer.SharedProjectionIntentRow,
	evidenceSource string,
) (string, map[string]any, bool) {
	switch domain {
	case reducer.DomainPlatformInfra:
		repoID := payloadString(row.Payload, "repo_id")
		platformID := payloadString(row.Payload, "platform_id")
		if repoID == "" || platformID == "" {
			return "", nil, false
		}
		return batchCanonicalInfrastructurePlatformUpsertCypher, map[string]any{
			"repo_id":              repoID,
			"platform_id":          platformID,
			"platform_name":        payloadString(row.Payload, "platform_name"),
			"platform_kind":        payloadString(row.Payload, "platform_kind"),
			"platform_provider":    payloadString(row.Payload, "platform_provider"),
			"platform_environment": payloadString(row.Payload, "platform_environment"),
			"platform_region":      payloadString(row.Payload, "platform_region"),
			"platform_locator":     payloadString(row.Payload, "platform_locator"),
			"evidence_source":      evidenceSource,
		}, true

	case reducer.DomainRepoDependency:
		repoID := payloadString(row.Payload, "repo_id")
		targetRepoID := payloadString(row.Payload, "target_repo_id")
		relationshipType := payloadString(row.Payload, "relationship_type")
		if repoID == "" {
			return "", nil, false
		}
		if relationshipType == "RUNS_ON" {
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
			return batchCanonicalRunsOnUpsertCypher, rowMap, true
		}
		if targetRepoID == "" {
			return "", nil, false
		}
		if relationshipType == "" || relationshipType == "DEPENDS_ON" {
			rowMap := map[string]any{
				"repo_id":         repoID,
				"target_repo_id":  targetRepoID,
				"evidence_source": evidenceSource,
			}
			if evidenceType := payloadString(row.Payload, "evidence_type"); evidenceType != "" {
				rowMap["evidence_type"] = evidenceType
			}
			copyRepoRelationshipMetadata(rowMap, row.Payload, row.GenerationID)
			return batchCanonicalRepoDependencyUpsertCypher, rowMap, true
		}
		rowMap := map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    targetRepoID,
			"relationship_type": relationshipType,
			"evidence_source":   evidenceSource,
		}
		if evidenceType := payloadString(row.Payload, "evidence_type"); evidenceType != "" {
			rowMap["evidence_type"] = evidenceType
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
		childEntityID := payloadString(row.Payload, "child_entity_id")
		parentEntityID := payloadString(row.Payload, "parent_entity_id")
		if childEntityID == "" || parentEntityID == "" {
			return "", nil, false
		}
		rowMap := map[string]any{
			"child_entity_id":   childEntityID,
			"parent_entity_id":  parentEntityID,
			"relationship_type": payloadString(row.Payload, "relationship_type"),
			"evidence_source":   evidenceSource,
		}
		switch rowMap["relationship_type"] {
		case "OVERRIDES":
			return batchCanonicalInheritanceOverrideUpsertCypher, rowMap, true
		case "ALIASES":
			return batchCanonicalInheritanceAliasUpsertCypher, rowMap, true
		case "", "INHERITS":
			return batchCanonicalInheritanceEdgeUpsertCypher, rowMap, true
		default:
			return "", nil, false
		}

	case reducer.DomainSQLRelationships:
		return buildSQLRelationshipRowMap(row.Payload, evidenceSource)

	default:
		return "", nil, false
	}
}

// RetractEdges retracts canonical domain edges for the given rows. Retraction
// collects repo IDs from all rows and executes one batched DELETE statement.
