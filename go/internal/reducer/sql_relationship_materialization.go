// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	log "github.com/eshu-hq/eshu/go/pkg/log"
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
	"SqlMigration",
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
// into durable shared-projection intent emission for READS_FROM, WRITES_TO,
// REFERENCES_TABLE, HAS_COLUMN, TRIGGERS, EXECUTES, and INDEXES edges. Each
// repository gets one whole-scope refresh intent that owns the retract, and
// each edge gets a write-only per-edge intent under a file-scoped partition
// key fenced behind that refresh (#2868).
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

	slog.InfoContext(
		ctx, "sql relationship materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := loadSQLRelationshipMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for sql relationship materialization: %w", err)
	}

	deltaScope := buildSQLRelationshipDeltaScope(envelopes)
	repositoryIDs, edgeRows, rowStats := ExtractSQLRelationshipRows(envelopes)
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

	slog.InfoContext(
		ctx, "sql relationship materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("edge_count", len(edgeRows)),
		slog.Int("repo_count", len(repositoryIDs)),
		// unresolved/ambiguous_read_targets surface READS_FROM source_tables
		// entries that resolveSQLReadTarget could not (or refused to) turn
		// into an edge, so an operator can see silent-empty risk without
		// re-deriving it from the graph (#5345).
		slog.Int("unresolved_read_targets", rowStats.UnresolvedReadTargets),
		slog.Int("ambiguous_read_targets", rowStats.AmbiguousReadTargets),
		slog.Int("unresolved_reference_targets", rowStats.UnresolvedReferenceTargets),
		slog.Int("ambiguous_reference_targets", rowStats.AmbiguousReferenceTargets),
		slog.Int("unresolved_write_targets", rowStats.UnresolvedWriteTargets),
		slog.Int("ambiguous_write_targets", rowStats.AmbiguousWriteTargets),
		// unresolved/ambiguous_migration_targets mirror the read-target pair
		// above for MIGRATES migration_targets entries resolveSQLMigrationTarget
		// could not (unresolved) or refused to (ambiguous, #5346 Trap 1) turn
		// into an edge.
		slog.Int("unresolved_migration_targets", rowStats.UnresolvedMigrationTargets),
		slog.Int("ambiguous_migration_targets", rowStats.AmbiguousMigrationTargets),
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
	case "SqlTable", "SqlColumn", "SqlView", "SqlFunction", "SqlTrigger", "SqlIndex", "SqlMigration":
		return true
	default:
		return false
	}
}

// ExtractSQLRelationshipRows builds canonical SQL relationship edge rows from
// content_entity fact envelopes. It builds an entity index from SQL entities,
// then derives edges from entity metadata (source_tables, table_name,
// function_name). The returned SQLRelationshipRowStats reports READS_FROM
// target resolutions that were skipped (unresolved or ambiguous) rather than
// silently dropped, so callers can log them (#5345).
func ExtractSQLRelationshipRows(
	envelopes []facts.Envelope,
) ([]string, []map[string]any, SQLRelationshipRowStats) {
	if len(envelopes) == 0 {
		return nil, nil, SQLRelationshipRowStats{}
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
		return nil, nil, SQLRelationshipRowStats{}
	}

	repoIDs := make([]string, 0, len(repoSet))
	for id := range repoSet {
		repoIDs = append(repoIDs, id)
	}
	sort.Strings(repoIDs)

	// Pass 2: derive edges from entity metadata.
	seenEdges := make(map[string]struct{})
	var rows []map[string]any
	var stats SQLRelationshipRowStats

	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		entityType := semanticPayloadString(env.Payload, "entity_type")
		entityID := semanticPayloadString(env.Payload, "entity_id")
		relativePath := semanticPayloadString(env.Payload, "relative_path")
		// entityPath is the repo-qualified path of THIS entity. It is the edge
		// source for READS_FROM/WRITES_TO/REFERENCES_TABLE/TRIGGERS/EXECUTES
		// (the iterated entity is the source), so it anchors the file-scoped
		// partition key and the source.path delta retract for those edges (#2868).
		// HAS_COLUMN's source is the resolved table, so that edge takes its
		// source_path from the table instead.
		entityPath := semanticPayloadString(env.Payload, "path")

		if repoID == "" || entityID == "" || !isSQLEntityType(entityType) {
			continue
		}

		metadata := payloadMap(env.Payload, "entity_metadata")
		source := sqlRelationshipEntity{
			entityID:     entityID,
			entityType:   entityType,
			repoID:       repoID,
			relativePath: relativePath,
			path:         entityPath,
		}

		switch entityType {
		case "SqlTable":
			var unresolved, ambiguous int
			rows, unresolved, ambiguous = appendSQLTableTargetRows(
				rows,
				seenEdges,
				entityByName,
				sqlMetadataStringSlice(metadata, "referenced_tables"),
				source,
				"REFERENCES_TABLE",
			)
			stats.UnresolvedReferenceTargets += unresolved
			stats.AmbiguousReferenceTargets += ambiguous

		case "SqlView", "SqlFunction":
			// source_tables metadata -> READS_FROM edges. Resolution tries a
			// SqlTable target first, then a SqlView target (so a view-on-view
			// direct read resolves), and is direct-only: no transitive closure
			// (#5345).
			sourceTables := sqlMetadataStringSlice(metadata, "source_tables")
			for _, tableName := range sourceTables {
				target, ambiguous, ok := resolveSQLReadTarget(entityByName, tableName, repoID, relativePath)
				if ambiguous {
					stats.AmbiguousReadTargets++
					continue
				}
				if !ok {
					stats.UnresolvedReadTargets++
					continue
				}
				edgeKey := entityID + "->READS_FROM->" + target.entityID
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
					"relationship_type":  "READS_FROM",
				})
			}
			if entityType == "SqlFunction" {
				var unresolved, ambiguous int
				rows, unresolved, ambiguous = appendSQLTableTargetRows(
					rows,
					seenEdges,
					entityByName,
					sqlMetadataStringSlice(metadata, "write_tables"),
					source,
					"WRITES_TO",
				)
				stats.UnresolvedWriteTargets += unresolved
				stats.AmbiguousWriteTargets += ambiguous
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

		case "SqlMigration":
			// migration_targets metadata -> MIGRATES edges (#5346). Each target
			// carries its own resolved kind (stamped by the parser from the
			// bucket it was captured in, or "SqlTable" for a bounded DML/ALTER/
			// REFERENCES/DROP mention). The operation remains target metadata;
			// MIGRATES represents migration adjacency/provenance and does not infer
			// a target's head-state presence or absence. Resolution is direct: no
			// SqlTable/SqlView dual-kind fallback the way READS_FROM needs. A same-kind,
			// same-name collision across files is reported as ambiguous rather
			// than guessed (Trap 1); a target naming nothing in the repo is
			// unresolved.
			for _, target := range sqlMetadataMapSlice(metadata, "migration_targets") {
				targetKind := anyToString(target["kind"])
				targetName := anyToString(target["name"])
				if targetKind == "" || targetName == "" {
					continue
				}
				resolved, ambiguous, ok := resolveSQLMigrationTarget(entityByName, targetName, targetKind, repoID, relativePath)
				if ambiguous {
					stats.AmbiguousMigrationTargets++
					continue
				}
				if !ok {
					stats.UnresolvedMigrationTargets++
					continue
				}
				edgeKey := entityID + "->MIGRATES->" + resolved.entityID
				if _, seen := seenEdges[edgeKey]; seen {
					continue
				}
				seenEdges[edgeKey] = struct{}{}
				rows = append(rows, map[string]any{
					"source_entity_id":   entityID,
					"target_entity_id":   resolved.entityID,
					"source_entity_type": entityType,
					"target_entity_type": resolved.entityType,
					"source_path":        entityPath,
					"repo_id":            repoID,
					"relationship_type":  "MIGRATES",
				})
			}

		case "SqlIndex":
			// table_name metadata -> INDEXES edge (index -> table). Mirrors the
			// SqlTrigger table_name resolution above; an index whose table_name
			// does not resolve to a present SqlTable is skipped rather than
			// fabricated (#5330 Task 3 — wires the blast-radius INDEXES branch
			// that has never had a writer).
			tableName := sqlMetadataString(metadata, "table_name")
			if tableName == "" {
				continue
			}
			target, ok := resolveSQLRelationshipTarget(entityByName, tableName, "SqlTable", repoID, relativePath)
			if !ok {
				continue
			}
			edgeKey := entityID + "->INDEXES->" + target.entityID
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
				"relationship_type":  "INDEXES",
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

	return repoIDs, rows, stats
}
