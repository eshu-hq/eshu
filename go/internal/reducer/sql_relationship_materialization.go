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

type sqlRelationshipEntity struct {
	entityID     string
	entityType   string
	repoID       string
	relativePath string
	// path is the repo-qualified file path of the entity, captured so the
	// promoted shared-projection path can place each edge under a file-scoped
	// partition key and so the file-scoped delta retract (which keys on
	// source.path) can target exactly the changed files (#2868).
	path string
}

// SQLRelationshipIntentWriter persists durable shared-projection intents for SQL
// relationship edge materialization (#2868). The promoted handler emits intents
// instead of writing edges directly so the #2755 partitioned runner projects
// them under file-scoped partition keys and the #2898 refresh fence owns the
// single per-repo retract.
type SQLRelationshipIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

// SQLRelationshipMaterializationHandler reduces one SQL relationship follow-up
// into durable shared-projection intent emission for REFERENCES_TABLE,
// HAS_COLUMN, TRIGGERS, and EXECUTES edges. Each repository gets one whole-scope
// refresh intent that owns the retract, and each edge gets a write-only per-edge
// intent under a file-scoped partition key fenced behind that refresh (#2868).
type SQLRelationshipMaterializationHandler struct {
	FactLoader   FactLoader
	IntentWriter SQLRelationshipIntentWriter
}

// Handle executes the SQL relationship materialization path.
func (h SQLRelationshipMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainSQLRelationshipMaterialization {
		return Result{}, fmt.Errorf(
			"sql relationship materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("sql relationship materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return Result{}, fmt.Errorf("sql relationship materialization intent writer is required")
	}

	slog.InfoContext(ctx, "sql relationship materialization started",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
	)

	envelopes, err := loadSQLRelationshipMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for sql relationship materialization: %w", err)
	}

	deltaScope := buildSQLRelationshipDeltaScope(envelopes)
	repositoryIDs, edgeRows := ExtractSQLRelationshipRows(envelopes)
	repositoryIDs = mergeSQLRelationshipRepositoryIDs(repositoryIDs, deltaScope.repositoryIDs)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, intent.GenerationID)
	if len(repositoryIDs) == 0 || len(contextByRepoID) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainSQLRelationshipMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for sql relationship materialization",
		}, nil
	}

	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentRows := buildSQLRelationshipSharedIntentRows(edgeRows, deltaScope, repositoryIDs, contextByRepoID, createdAt)
	if len(intentRows) > 0 {
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			return Result{}, fmt.Errorf("write sql relationship intents: %w", err)
		}
	}

	slog.InfoContext(ctx, "sql relationship materialization completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("edge_count", len(edgeRows)),
		slog.Int("repo_count", len(repositoryIDs)),
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainSQLRelationshipMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable sql relationship intents across %d repositories",
			len(intentRows),
			len(repositoryIDs),
		),
		CanonicalWrites: len(intentRows),
	}, nil
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
// then derives edges from entity metadata (source_tables, table_name,
// function_name).
func ExtractSQLRelationshipRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}
	repoSet := make(map[string]struct{})
	entityByName := make(map[string][]sqlRelationshipEntity)

	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		entityType := semanticPayloadString(env.Payload, "entity_type")
		entityID := semanticPayloadString(env.Payload, "entity_id")
		entityName := semanticPayloadString(env.Payload, "entity_name")
		relativePath := semanticPayloadString(env.Payload, "relative_path")

		if repoID == "" || entityID == "" || entityName == "" {
			continue
		}
		if !isSQLEntityType(entityType) {
			continue
		}

		repoSet[repoID] = struct{}{}
		entity := sqlRelationshipEntity{
			entityID:     entityID,
			entityType:   entityType,
			repoID:       repoID,
			relativePath: relativePath,
			path:         semanticPayloadString(env.Payload, "path"),
		}
		addSQLRelationshipEntityIndex(entityByName, entityName, entity)
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
		relativePath := semanticPayloadString(env.Payload, "relative_path")
		// entityPath is the repo-qualified path of THIS entity. It is the edge
		// source for REFERENCES_TABLE/TRIGGERS/EXECUTES (the iterated entity is the
		// source), so it anchors the file-scoped partition key and the source.path
		// delta retract for those edges (#2868). HAS_COLUMN's source is the resolved
		// table, so that edge takes its source_path from the table instead.
		entityPath := semanticPayloadString(env.Payload, "path")

		if repoID == "" || entityID == "" || !isSQLEntityType(entityType) {
			continue
		}

		metadata := payloadMap(env.Payload, "entity_metadata")

		switch entityType {
		case "SqlView", "SqlFunction":
			// source_tables metadata -> REFERENCES_TABLE edges
			sourceTables := sqlMetadataStringSlice(metadata, "source_tables")
			for _, tableName := range sourceTables {
				target, ok := resolveSQLRelationshipTarget(entityByName, tableName, "SqlTable", repoID, relativePath)
				if !ok {
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
					"source_path":        entityPath,
					"repo_id":            repoID,
					"relationship_type":  "REFERENCES_TABLE",
				})
			}

		case "SqlTrigger":
			// table_name metadata -> TRIGGERS edge
			tableName := sqlMetadataString(metadata, "table_name")
			if tableName != "" {
				target, ok := resolveSQLRelationshipTarget(entityByName, tableName, "SqlTable", repoID, relativePath)
				if ok {
					edgeKey := entityID + "->TRIGGERS->" + target.entityID
					if _, seen := seenEdges[edgeKey]; !seen {
						seenEdges[edgeKey] = struct{}{}
						rows = append(rows, map[string]any{
							"source_entity_id":   entityID,
							"target_entity_id":   target.entityID,
							"source_entity_type": entityType,
							"target_entity_type": target.entityType,
							"source_path":        entityPath,
							"repo_id":            repoID,
							"relationship_type":  "TRIGGERS",
						})
					}
				}
			}

			// function_name metadata -> EXECUTES edge.
			functionName := sqlMetadataString(metadata, "function_name")
			if functionName == "" {
				continue
			}
			target, ok := resolveSQLRelationshipTarget(entityByName, functionName, "SqlFunction", repoID, relativePath)
			if !ok {
				continue
			}
			edgeKey := entityID + "->EXECUTES->" + target.entityID
			if _, seen := seenEdges[edgeKey]; seen {
				continue
			}
			seenEdges[edgeKey] = struct{}{}
			rows = append(rows, map[string]any{
				"source_entity_id":   entityID,
				"target_entity_id":   target.entityID,
				"source_entity_type": entityType,
				"target_entity_type": target.entityType,
				"source_path":        entityPath,
				"repo_id":            repoID,
				"relationship_type":  "EXECUTES",
			})

		case "SqlColumn":
			// table_name metadata -> HAS_COLUMN edge (table -> column)
			tableName := sqlMetadataString(metadata, "table_name")
			if tableName == "" {
				continue
			}
			source, ok := resolveSQLRelationshipTarget(entityByName, tableName, "SqlTable", repoID, relativePath)
			if !ok {
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
				// HAS_COLUMN's edge source is the TABLE, so its source_path is the
				// table's repo-qualified path, not the iterated column's (#2868).
				"source_path":       source.path,
				"repo_id":           repoID,
				"relationship_type": "HAS_COLUMN",
			})
		}
	}
	rows = appendEmbeddedSQLQueryRows(rows, seenEdges, entityByName, envelopes)

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
