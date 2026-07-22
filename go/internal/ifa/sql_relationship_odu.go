// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// SQL family Odù constants (#5351): the fixture proving the
// materialized_edges:sql_relationships surface. sqlFamilyRepoID and
// sqlFamilyScopeID anchor every fact in the fixture; sqlFamilySchemaPath is
// the single db/schema.sql source every SQL entity below carries as its
// relative_path, mirroring a real migration/schema file a SQL parser would
// emit multiple entities from.
const (
	sqlFamilyOduName      = "odu:ifa-sql-family"
	sqlFamilyDeltaOduName = "odu:ifa-sql-family-delta"
	sqlFamilyRepoID       = "repo-ifa-sql-family"
	sqlFamilyScopeID      = "scope-ifa-sql-family"
	sqlFamilyGenerationID = "gen-1"
	sqlFamilyDeltaGenID   = "gen-2"
	sqlFamilySchemaPath   = "db/schema.sql"
	sqlFamilyHandlerPath  = "cmd/api/handlers.go"
	sqlFamilySourceRunID  = "run-ifa-sql-family-1"

	// sqlFamilyLocalPath is the repository fact's local_path — the checkout
	// path the real git collector emits (repositoryFactEnvelope's
	// payload["local_path"] = repo.LocalPath). It is REQUIRED for the delta
	// retract to work, and its absence was the #5549 P1a live-proof finding:
	// the projector derives every entity NODE's `path` property as
	// qualify(repoPath, relative_path) where repoPath falls back to
	// local_path (the collector never emits a top-level "path" — see
	// projector/canonical_codegraph_extract.go's "collector does not emit
	// path" comment), and the file-scoped SQL delta retract anchors on that
	// node `path` property (edge_writer_sql.go's
	// `MATCH (source:SqlIndex {path: file_path})`). With no local_path the
	// node path is unqualified AND deltaScope.filePathsByRepoID is empty, so
	// the retract matches nothing and a retargeted INDEXES edge leaves its
	// stale predecessor in the graph. "/repo" mirrors the reducer's own delta
	// test (sql_relationship_delta_scope_test.go).
	sqlFamilyLocalPath = "/repo"

	// sqlFamilyGetUserFunctionUID is content.CanonicalEntityID(sqlFamilyRepoID,
	// sqlFamilyHandlerPath, "Function", "GetUser", 10)
	// (go/internal/content/writer.go): the canonical graph uid the projector's
	// canonical entity writer derives for a "Function"-labeled node
	// (projector/canonical_entity_identity.go's canonicalNamePathLineEntityLabels
	// includes "Function", so its incoming content_entity entity_id is IGNORED
	// at write time in favor of this hash). QUERIES_TABLE's source endpoint is
	// this Function node, so both the content_entity fact below AND
	// parsed_file_data.functions[].uid in sqlFamilyFileWithEmbeddedQuery must
	// use this SAME precomputed value — proven live (#5351 claim 3
	// verification): a hand-picked "content-entity:fn-get-user" id, while
	// perfectly valid input to the pure reducer.ExtractSQLRelationshipRows seam,
	// does not match the graph's actual canonically-derived Function uid, so the
	// QUERIES_TABLE edge write's MATCH silently no-ops against a real backend
	// even though ExtractSQLRelationshipRows still (correctly) derives the row.
	sqlFamilyGetUserFunctionUID = "content-entity:e_cb021b7a4238"
)

// sqlFamilyOdu carries one repository fact, a second-table-plus-six-more SQL
// content_entity set, and one file fact with an embedded SQL query, wired so
// reducer.ExtractSQLRelationshipRows (go/internal/reducer/sql_relationship_
// materialization.go) derives exactly one edge of each of the nine
// materialized SQL relationship types (cypher.SQLRelationshipMaterializedEdgeTypes,
// go/internal/storage/cypher/edge_writer_sql.go): QUERIES_TABLE, READS_FROM,
// REFERENCES_TABLE, WRITES_TO, HAS_COLUMN, TRIGGERS, EXECUTES, INDEXES, and
// MIGRATES.
//
// The repository fact carries both repo_id and source_run_id: without
// source_run_id, buildCodeCallProjectionContexts (code_call_materialization_
// intents.go:59-65) yields no projection context and the handler exits
// early ("no repositories available"), so every edge would silently fail to
// materialize even though ExtractSQLRelationshipRows itself derived them.
//
// content_entity payloads deliberately omit a top-level "path" key: the real
// git collector's contentEntityFactEnvelope (go/internal/collector/
// git_content_fact_envelopes.go) never sets one either, only relative_path —
// so entityPath/source_path on every derived edge is empty, matching
// production, not a test-only convenience field. The file fact instead
// carries the embedded query's source path via parsed_file_data.path
// (payloadPath(fileData, "path") in the real fileFactEnvelope emitter),
// which embeddedSQLQuerySources falls back to when the top-level path is
// blank (sql_relationship_embedded_query.go).
func sqlFamilyOdu() CatalogOdu {
	odu := Odu{
		Name: sqlFamilyOduName,
		Facts: []facts.Envelope{
			sqlFamilyRepositoryFact(sqlFamilyGenerationID, false, nil),
			sqlFamilySchemaFileFact(sqlFamilyGenerationID),
			sqlFamilyGetUserFunctionEntity(sqlFamilyGenerationID),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-tbl-users", "SqlTable", "public.users", map[string]any{
				"referenced_tables": []string{"public.orders"},
				"sql_entity_type":   "SqlTable",
			}),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-tbl-orders", "SqlTable", "public.orders", nil),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-col-email", "SqlColumn", "public.users.email", map[string]any{
				"table_name":      "public.users",
				"sql_entity_type": "SqlColumn",
			}),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-view-active-users", "SqlView", "public.active_users", map[string]any{
				"source_tables":   []string{"public.users"},
				"sql_entity_type": "SqlView",
			}),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-fn-touch-updated-at", "SqlFunction", "public.touch_updated_at", map[string]any{
				"write_tables":    []string{"public.users"},
				"sql_entity_type": "SqlFunction",
			}),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-trig-users-touch", "SqlTrigger", "users_touch", map[string]any{
				"table_name":      "public.users",
				"function_name":   "public.touch_updated_at",
				"sql_entity_type": "SqlTrigger",
			}),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-idx-users-email", "SqlIndex", "idx_users_email", map[string]any{
				"table_name":      "public.users",
				"sql_entity_type": "SqlIndex",
			}),
			sqlFamilyContentEntity(sqlFamilyGenerationID, "content-entity:sql-mig-v1-add-users", "SqlMigration", "V1__add_users", map[string]any{
				"tool":            "flyway",
				"sql_entity_type": "SqlMigration",
				"migration_targets": []map[string]any{
					{"kind": "SqlTable", "name": "public.users", "operation": "create", "line_number": 1},
				},
			}),
			sqlFamilyFileWithEmbeddedQuery(sqlFamilyGenerationID),
			sqlFamilyFollowupFact(sqlFamilyGenerationID),
		},
	}
	return CatalogOdu{
		Odu:    odu,
		Detail: "one repo, two SqlTable, one SqlColumn/SqlView/SqlFunction/SqlTrigger/SqlIndex/SqlMigration each, one embedded-SQL-query file, and the production shared_followup trigger fact, deriving exactly one edge of each of the nine materialized SQL relationship types (QUERIES_TABLE/READS_FROM/REFERENCES_TABLE/WRITES_TO/HAS_COLUMN/TRIGGERS/EXECUTES/INDEXES/MIGRATES)",
	}
}

// sqlFamilyDeltaOdu is the gen-2 delta variant (#5351 P4 cell 7): a delta
// re-collection of db/schema.sql only (delta_generation, delta_relative_paths
// = [db/schema.sql]) that retargets the SqlIndex from public.users to
// public.orders. It re-emits every SQL entity db/schema.sql still carries
// (tables, column, view, trigger, migration) because a real delta
// re-collection re-parses the whole changed file, not one entity in
// isolation — and every one of those entities is still required in the SAME
// generation for their own edges to resolve (loadSQLRelationshipMaterialization
// Facts scopes ListFactsByKind to one (scopeID, generationID) pair). It does
// NOT re-emit the handlers.go file fact: that file did not change, so its
// QUERIES_TABLE edge is expected to persist only through the durable
// file-scoped partition the live P4 proof asserts over the accumulated graph
// (graphdump), not through this generation's own pure
// ExtractSQLRelationshipRows output — see materialized_edges_sql.go's doc
// comment for the generation-local vs. accumulated-graph distinction.
func sqlFamilyDeltaOdu() CatalogOdu {
	odu := Odu{
		Name: sqlFamilyDeltaOduName,
		Facts: []facts.Envelope{
			sqlFamilyRepositoryFact(sqlFamilyDeltaGenID, true, []string{sqlFamilySchemaPath}),
			sqlFamilySchemaFileFact(sqlFamilyDeltaGenID),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-tbl-users", "SqlTable", "public.users", map[string]any{
				"referenced_tables": []string{"public.orders"},
				"sql_entity_type":   "SqlTable",
			}),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-tbl-orders", "SqlTable", "public.orders", nil),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-col-email", "SqlColumn", "public.users.email", map[string]any{
				"table_name":      "public.users",
				"sql_entity_type": "SqlColumn",
			}),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-view-active-users", "SqlView", "public.active_users", map[string]any{
				"source_tables":   []string{"public.users"},
				"sql_entity_type": "SqlView",
			}),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-fn-touch-updated-at", "SqlFunction", "public.touch_updated_at", map[string]any{
				"write_tables":    []string{"public.users"},
				"sql_entity_type": "SqlFunction",
			}),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-trig-users-touch", "SqlTrigger", "users_touch", map[string]any{
				"table_name":      "public.users",
				"function_name":   "public.touch_updated_at",
				"sql_entity_type": "SqlTrigger",
			}),
			// Retargeted: idx_users_email now indexes public.orders, not
			// public.users (the #5351 delta-retract teeth).
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-idx-users-email", "SqlIndex", "idx_users_email", map[string]any{
				"table_name":      "public.orders",
				"sql_entity_type": "SqlIndex",
			}),
			sqlFamilyContentEntity(sqlFamilyDeltaGenID, "content-entity:sql-mig-v1-add-users", "SqlMigration", "V1__add_users", map[string]any{
				"tool":            "flyway",
				"sql_entity_type": "SqlMigration",
				"migration_targets": []map[string]any{
					{"kind": "SqlTable", "name": "public.users", "operation": "create", "line_number": 1},
				},
			}),
			sqlFamilyFollowupFact(sqlFamilyDeltaGenID),
		},
	}
	return CatalogOdu{
		Odu:    odu,
		Detail: "gen-2 delta re-collection of db/schema.sql retargeting INDEXES from public.users to public.orders, proving the delta-retract path for the SQL relationship family",
	}
}

// sqlFamilyFollowupFact is the production trigger fact
// (go/internal/collector/git_followup_facts.go's
// sqlRelationshipMaterializationFactEnvelope): the shared_followup fact whose
// reducer_domain: sql_relationship_materialization payload key is what
// go/internal/projector's buildReducerIntent turns into a durable
// fact_work_items row under the real pipeline (and under `ifa drive` driving
// the live JSON cassette). ExtractSQLRelationshipRows itself ignores this
// fact kind (it only reads content_entity/file), so it is inert for the pure
// vacuity guard but is included for fidelity with the live-drive cassette and
// the real collector's own emission shape.
func sqlFamilyFollowupFact(generationID string) facts.Envelope {
	return facts.Envelope{
		ScopeID:      sqlFamilyScopeID,
		GenerationID: generationID,
		FactKind:     sharedFollowupFactKind,
		Payload: map[string]any{
			"reducer_domain": "sql_relationship_materialization",
			"entity_key":     "sql:" + sqlFamilyRepoID,
			"reason":         "repository snapshot emitted SQL relationship materialization follow-up",
			"repo_id":        sqlFamilyRepoID,
		},
	}
}

func sqlFamilyRepositoryFact(generationID string, delta bool, deltaRelativePaths []string) facts.Envelope {
	payload := map[string]any{
		"repo_id":       sqlFamilyRepoID,
		"source_run_id": sqlFamilySourceRunID,
		// local_path qualifies every entity node's path property (the delta
		// retract anchor) — see sqlFamilyLocalPath's doc comment (#5549 P1a).
		"local_path": sqlFamilyLocalPath,
	}
	if delta {
		payload["delta_generation"] = true
		if len(deltaRelativePaths) > 0 {
			payload["delta_relative_paths"] = deltaRelativePaths
		}
	}
	return facts.Envelope{
		ScopeID:      sqlFamilyScopeID,
		GenerationID: generationID,
		FactKind:     repositoryFactKind,
		Payload:      payload,
	}
}

// sqlFamilySchemaFileFact carries the "file" fact for db/schema.sql itself
// (#5351 live-proof finding): every SQL content_entity fact's containment
// write (cypher.canonicalNodeEntityUpsertWithContainmentTemplate,
// go/internal/storage/cypher/canonical_node_cypher.go) is
// `UNWIND $rows AS row MATCH (f:File {path: row.file_path}) MERGE (n:%s
// {uid: row.entity_id}) ...` — an UNWIND+MATCH INNER JOIN. Without a File
// node at db/schema.sql already present, every row's MATCH fails and the
// whole UNWIND produces zero iterations, so the MERGE never runs for ANY of
// the eight SQL entities: the entity node write is a SILENT no-op with no
// error and a misleadingly successful "entity label summary complete=true"
// log line (the log describes the attempted batch, not the per-row MATCH
// outcome). This was reproduced live against a real Postgres+NornicDB stack
// while verifying #5351's flagged claim 3 (endpoint-node projection) before
// this fixture existed with the fact below: proof that "an edge MATCH with a
// missing endpoint node is a silent no-op" (the spec's own caution) applies
// one level earlier too, at node-containment write time, not only at
// edge-write time.
func sqlFamilySchemaFileFact(generationID string) facts.Envelope {
	return facts.Envelope{
		ScopeID:      sqlFamilyScopeID,
		GenerationID: generationID,
		FactKind:     fileFactKind,
		Payload: map[string]any{
			"repo_id":          sqlFamilyRepoID,
			"relative_path":    sqlFamilySchemaPath,
			"parsed_file_data": map[string]any{},
		},
	}
}

// sqlFamilyGetUserFunctionEntity carries the content_entity fact for the
// "GetUser" Function QUERIES_TABLE's source endpoint (#5351 live-proof
// finding, see sqlFamilyGetUserFunctionUID's doc comment): a plain content_entity
// fact with entity_type "Function" is what actually materializes a graph
// Function node (the parsed_file_data.functions[] entry in
// sqlFamilyFileWithEmbeddedQuery alone does NOT — that array is read only by
// this package's own embeddedSQLFunctionIDsByNameLine convenience lookup, not
// by the projector's canonical entity writer). Its entity_id is set to the
// same precomputed canonical uid the label="Function" branch of
// canonicalGraphEntityID derives regardless of what this payload supplies,
// so the fact stays internally consistent even though the write path ignores
// it.
func sqlFamilyGetUserFunctionEntity(generationID string) facts.Envelope {
	return facts.Envelope{
		ScopeID:      sqlFamilyScopeID,
		GenerationID: generationID,
		FactKind:     contentEntityFactKind,
		Payload: map[string]any{
			"repo_id":       sqlFamilyRepoID,
			"entity_id":     sqlFamilyGetUserFunctionUID,
			"entity_type":   "Function",
			"entity_name":   "GetUser",
			"relative_path": sqlFamilyHandlerPath,
			"start_line":    10,
			"end_line":      20,
		},
	}
}

func sqlFamilyContentEntity(generationID, entityID, entityType, entityName string, metadata map[string]any) facts.Envelope {
	payload := map[string]any{
		"repo_id":       sqlFamilyRepoID,
		"entity_id":     entityID,
		"entity_type":   entityType,
		"entity_name":   entityName,
		"relative_path": sqlFamilySchemaPath,
	}
	if metadata != nil {
		payload["entity_metadata"] = metadata
	}
	return facts.Envelope{
		ScopeID:      sqlFamilyScopeID,
		GenerationID: generationID,
		FactKind:     contentEntityFactKind,
		Payload:      payload,
	}
}

// sqlFamilyFileWithEmbeddedQuery carries the one function/embedded-query pair
// that derives the QUERIES_TABLE edge, mirroring
// sql_relationship_embedded_query_test.go's sqlRelationshipFileWithEmbeddedQuery
// fixture shape: parsed_file_data.path (not the top-level payload path) is what
// embeddedSQLQuerySources falls back to, matching what the real fileFactEnvelope
// emitter (go/internal/collector/git_fact_builder.go) actually produces.
func sqlFamilyFileWithEmbeddedQuery(generationID string) facts.Envelope {
	return facts.Envelope{
		ScopeID:      sqlFamilyScopeID,
		GenerationID: generationID,
		FactKind:     fileFactKind,
		Payload: map[string]any{
			"repo_id":       sqlFamilyRepoID,
			"relative_path": sqlFamilyHandlerPath,
			"parsed_file_data": map[string]any{
				"path": "/repo/" + sqlFamilyHandlerPath,
				"functions": []map[string]any{
					{
						"name":        "GetUser",
						"uid":         sqlFamilyGetUserFunctionUID,
						"line_number": 10,
						"end_line":    20,
					},
				},
				"embedded_sql_queries": []map[string]any{
					{
						"function_name":        "GetUser",
						"function_line_number": 10,
						"table_name":           "public.users",
						"operation":            "select",
						"line_number":          13,
						"api":                  "database/sql",
					},
				},
			},
		},
	}
}
