package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	sqlRelationshipEvidenceSource = "reducer/sql-relationships"
)

var sqlRelationshipContentEntityTypes = []string{
	"SqlTable",
	"SqlColumn",
	"SqlView",
	"SqlFunction",
	"SqlTrigger",
	"SqlIndex",
}

// SQLRelationshipMaterializationHandler reduces one SQL relationship follow-up
// into canonical SQL edge writes (REFERENCES_TABLE, HAS_COLUMN, TRIGGERS).
type SQLRelationshipMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
}

// Handle executes the SQL relationship materialization path.
func (h SQLRelationshipMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainSQLRelationshipMaterialization {
		return Result{}, fmt.Errorf(
			"sql relationship materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("sql relationship materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("sql relationship materialization edge writer is required")
	}

	slog.InfoContext(ctx, "sql relationship materialization started",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
	)

	loadStart := time.Now()
	envelopes, err := loadFactsForKindAndPayloadValue(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		factKindContentEntity,
		"entity_type",
		sqlRelationshipContentEntityTypes,
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for sql relationship materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	repositoryIDs, edgeRows := ExtractSQLRelationshipRows(envelopes)
	extractDuration := time.Since(extractStart)
	if len(repositoryIDs) == 0 {
		logSQLRelationshipMaterializationCompleted(ctx, sqlRelationshipMaterializationTiming{
			intent:          intent,
			factCount:       len(envelopes),
			repoCount:       0,
			edgeCount:       0,
			writeRowCount:   0,
			loadDuration:    loadDuration,
			extractDuration: extractDuration,
			totalDuration:   time.Since(totalStart),
		})
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainSQLRelationshipMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for sql relationship materialization",
		}, nil
	}

	retractRows := buildSQLRelRetractRows(repositoryIDs)
	skipRetract, err := h.shouldSkipSQLRelationshipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if skipRetract {
		slog.InfoContext(ctx, "sql relationship materialization skipped first-generation retract",
			slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
		)
	} else {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractEdges(
			ctx,
			DomainSQLRelationships,
			retractRows,
			sqlRelationshipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical sql relationships: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	buildWriteStart := time.Now()
	writeRows := buildSQLRelIntentRows(edgeRows)
	buildWriteRowsDuration := time.Since(buildWriteStart)
	var graphWriteDuration time.Duration
	if len(writeRows) > 0 {
		graphWriteStart := time.Now()
		if err := h.EdgeWriter.WriteEdges(
			ctx,
			DomainSQLRelationships,
			writeRows,
			sqlRelationshipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical sql relationships: %w", err)
		}
		graphWriteDuration = time.Since(graphWriteStart)
	}

	logSQLRelationshipMaterializationCompleted(ctx, sqlRelationshipMaterializationTiming{
		intent:                 intent,
		factCount:              len(envelopes),
		repoCount:              len(repositoryIDs),
		edgeCount:              len(edgeRows),
		writeRowCount:          len(writeRows),
		skipRetract:            skipRetract,
		loadDuration:           loadDuration,
		extractDuration:        extractDuration,
		retractDuration:        retractDuration,
		buildWriteRowsDuration: buildWriteRowsDuration,
		graphWriteDuration:     graphWriteDuration,
		totalDuration:          time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainSQLRelationshipMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical sql relationship edges across %d repositories",
			len(writeRows),
			len(repositoryIDs),
		),
		CanonicalWrites: len(writeRows),
	}, nil
}

// sqlRelationshipMaterializationTiming groups stage durations so the
// completion log can identify whether SQL work is fact loading, extraction,
// retract, write-row shaping, or graph backend time.
type sqlRelationshipMaterializationTiming struct {
	intent                 Intent
	factCount              int
	repoCount              int
	edgeCount              int
	writeRowCount          int
	skipRetract            bool
	loadDuration           time.Duration
	extractDuration        time.Duration
	retractDuration        time.Duration
	buildWriteRowsDuration time.Duration
	graphWriteDuration     time.Duration
	totalDuration          time.Duration
}

func logSQLRelationshipMaterializationCompleted(
	ctx context.Context,
	timing sqlRelationshipMaterializationTiming,
) {
	slog.InfoContext(ctx, "sql relationship materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("repo_count", timing.repoCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.Int("write_row_count", timing.writeRowCount),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("build_write_rows_duration_seconds", timing.buildWriteRowsDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.graphWriteDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

func (h SQLRelationshipMaterializationHandler) shouldSkipSQLRelationshipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for sql relationship retract: %w", err)
	}
	return !hasPrior, nil
}

// isSQLEntityType reports whether the entity type is a known SQL entity.
func isSQLEntityType(entityType string) bool {
	switch entityType {
	case "SqlTable", "SqlColumn", "SqlView", "SqlFunction", "SqlTrigger", "SqlIndex":
		return true
	default:
		return false
	}
}

// ExtractSQLRelationshipRows builds canonical SQL relationship edge rows from
// content_entity fact envelopes. It builds an entity index from SQL entities,
// then derives edges from entity metadata (source_tables, table_name).
func ExtractSQLRelationshipRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	// Pass 1: collect repository IDs and build entity index by qualified name.
	type sqlEntity struct {
		entityID   string
		entityType string
		repoID     string
	}

	repoSet := make(map[string]struct{})
	entityByName := make(map[string]sqlEntity)

	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		entityType := semanticPayloadString(env.Payload, "entity_type")
		entityID := semanticPayloadString(env.Payload, "entity_id")
		entityName := semanticPayloadString(env.Payload, "entity_name")

		if repoID == "" || entityID == "" || entityName == "" {
			continue
		}
		if !isSQLEntityType(entityType) {
			continue
		}

		repoSet[repoID] = struct{}{}
		entityByName[entityName] = sqlEntity{
			entityID:   entityID,
			entityType: entityType,
			repoID:     repoID,
		}
	}

	if len(repoSet) == 0 {
		return nil, nil
	}

	repoIDs := make([]string, 0, len(repoSet))
	for id := range repoSet {
		repoIDs = append(repoIDs, id)
	}
	sort.Strings(repoIDs)

	// Pass 2: derive edges from entity metadata.
	seenEdges := make(map[string]struct{})
	var rows []map[string]any

	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		entityType := semanticPayloadString(env.Payload, "entity_type")
		entityID := semanticPayloadString(env.Payload, "entity_id")

		if repoID == "" || entityID == "" || !isSQLEntityType(entityType) {
			continue
		}

		metadata := payloadMap(env.Payload, "entity_metadata")

		switch entityType {
		case "SqlView", "SqlFunction":
			// source_tables metadata -> REFERENCES_TABLE edges
			sourceTables := sqlMetadataStringSlice(metadata, "source_tables")
			for _, tableName := range sourceTables {
				target, ok := entityByName[tableName]
				if !ok || target.entityType != "SqlTable" {
					continue
				}
				edgeKey := entityID + "->REFERENCES_TABLE->" + target.entityID
				if _, seen := seenEdges[edgeKey]; seen {
					continue
				}
				seenEdges[edgeKey] = struct{}{}
				rows = append(rows, map[string]any{
					"source_entity_id":   entityID,
					"target_entity_id":   target.entityID,
					"source_entity_type": entityType,
					"target_entity_type": target.entityType,
					"repo_id":            repoID,
					"relationship_type":  "REFERENCES_TABLE",
				})
			}

		case "SqlTrigger":
			// table_name metadata -> TRIGGERS edge
			tableName := sqlMetadataString(metadata, "table_name")
			if tableName == "" {
				continue
			}
			target, ok := entityByName[tableName]
			if !ok || target.entityType != "SqlTable" {
				continue
			}
			edgeKey := entityID + "->TRIGGERS->" + target.entityID
			if _, seen := seenEdges[edgeKey]; seen {
				continue
			}
			seenEdges[edgeKey] = struct{}{}
			rows = append(rows, map[string]any{
				"source_entity_id":   entityID,
				"target_entity_id":   target.entityID,
				"source_entity_type": entityType,
				"target_entity_type": target.entityType,
				"repo_id":            repoID,
				"relationship_type":  "TRIGGERS",
			})

		case "SqlColumn":
			// table_name metadata -> HAS_COLUMN edge (table -> column)
			tableName := sqlMetadataString(metadata, "table_name")
			if tableName == "" {
				continue
			}
			source, ok := entityByName[tableName]
			if !ok || source.entityType != "SqlTable" {
				continue
			}
			edgeKey := source.entityID + "->HAS_COLUMN->" + entityID
			if _, seen := seenEdges[edgeKey]; seen {
				continue
			}
			seenEdges[edgeKey] = struct{}{}
			rows = append(rows, map[string]any{
				"source_entity_id":   source.entityID,
				"target_entity_id":   entityID,
				"source_entity_type": source.entityType,
				"target_entity_type": entityType,
				"repo_id":            repoID,
				"relationship_type":  "HAS_COLUMN",
			})
		}
	}

	// Sort for deterministic output.
	sort.Slice(rows, func(i, j int) bool {
		left := anyToString(rows[i]["relationship_type"]) + ":" +
			anyToString(rows[i]["source_entity_id"]) + "->" +
			anyToString(rows[i]["target_entity_id"])
		right := anyToString(rows[j]["relationship_type"]) + ":" +
			anyToString(rows[j]["source_entity_id"]) + "->" +
			anyToString(rows[j]["target_entity_id"])
		return left < right
	})

	return repoIDs, rows
}

// sqlMetadataString extracts a string value from SQL entity metadata.
func sqlMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// sqlMetadataStringSlice extracts a string slice from SQL entity metadata.
func sqlMetadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return nil
	}
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok || s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

// buildSQLRelRetractRows builds retract intent rows for the given repository IDs.
func buildSQLRelRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		rows = append(rows, SharedProjectionIntentRow{RepositoryID: repositoryID})
	}
	return rows
}

// buildSQLRelIntentRows converts extracted edge maps into shared projection
// intent rows.
func buildSQLRelIntentRows(edgeRows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(edgeRows))
	for _, row := range edgeRows {
		sourceID := anyToString(row["source_entity_id"])
		targetID := anyToString(row["target_entity_id"])
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainSQLRelationships,
			PartitionKey:     sourceID + "->" + targetID,
			RepositoryID:     anyToString(row["repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}
