// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// nornicDBRelationshipEndpointMeta is the file and repository metadata for one
// relationship endpoint, fetched by the OPTIONAL-MATCH-free enrichment reads and
// merged onto the relationship core rows in Go.
type nornicDBRelationshipEndpointMeta struct {
	filePath     string
	fileLanguage string
	repoID       string
	repoName     string
}

// enrichNornicDBRelationshipRows attaches file and repository metadata to the
// relationship core rows. The core read (nornicDBOneHopRelationshipsCypher)
// deliberately omits OPTIONAL MATCH so its function-call projections evaluate
// correctly on the pinned NornicDB build; this restores the file/repo/language
// columns that used to ride those OPTIONAL MATCH clauses, using
// OPTIONAL-MATCH-free, index-anchored path reads whose results are joined to the
// core rows by endpoint identity (coalesce(id, uid)).
//
// File and repository metadata are read as SEPARATE reads rather than one
// File->Repository path so that an endpoint with a File but no REPO_CONTAINS edge
// (a partially projected graph) still contributes its file path and language,
// exactly as the pre-split OPTIONAL MATCH clauses did — a mandatory File->Repo
// path would drop the file metadata along with the absent repository.
//
// Enrichment is skipped entirely when the core rows carry no endpoint identity
// (the shape unit-test fakes produce), so those callers issue no extra query.
func (h *CodeHandler) enrichNornicDBRelationshipRows(
	ctx context.Context,
	rows []map[string]any,
	entityID string,
	direction string,
	relationshipType string,
	entityLabel string,
	entityIDProperty string,
) ([]map[string]any, error) {
	meta := make(map[string]nornicDBRelationshipEndpointMeta)
	if nornicDBRelationshipRowsHaveEndpointUIDs(rows) {
		params := map[string]any{"entity_id": entityID, "row_limit": nornicDBRelationshipFetchLimit}
		for _, cypher := range []string{
			nornicDBRelationshipFarFileEnrichmentCypher(direction, relationshipType, entityLabel, entityIDProperty),
			nornicDBRelationshipFarRepoEnrichmentCypher(direction, relationshipType, entityLabel, entityIDProperty),
			nornicDBRelationshipAnchorFileEnrichmentCypher(entityLabel, entityIDProperty),
			nornicDBRelationshipAnchorRepoEnrichmentCypher(entityLabel, entityIDProperty),
		} {
			enrichRows, err := h.Neo4j.Run(ctx, cypher, params)
			if err != nil {
				return nil, err
			}
			collectNornicDBEnrichment(meta, enrichRows)
		}
	}

	enriched := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		merged := cloneQueryAnyMap(row)
		applyNornicDBEndpointMeta(merged, "source", meta[StringVal(merged, "source_entity_uid")])
		applyNornicDBEndpointMeta(merged, "target", meta[StringVal(merged, "target_entity_uid")])
		delete(merged, "source_entity_uid")
		delete(merged, "target_entity_uid")
		enriched = append(enriched, merged)
	}
	return enriched, nil
}

func nornicDBRelationshipRowsHaveEndpointUIDs(rows []map[string]any) bool {
	for _, row := range rows {
		if StringVal(row, "source_entity_uid") != "" || StringVal(row, "target_entity_uid") != "" {
			return true
		}
	}
	return false
}

// collectNornicDBEnrichment folds enrichment rows into the identity-keyed
// metadata map, merging field by field. The file and repository reads each
// contribute only their own columns, so a File-only endpoint keeps its file
// metadata even though it never appears in the repository read. The first
// non-empty value per field wins, which keeps the merge deterministic even if a
// store carries duplicate File/Repository edges for a canonical entity.
func collectNornicDBEnrichment(meta map[string]nornicDBRelationshipEndpointMeta, rows []map[string]any) {
	for _, row := range rows {
		uid := StringVal(row, "entity_uid")
		if uid == "" {
			continue
		}
		endpoint := meta[uid]
		if v := StringVal(row, "file_path"); v != "" && endpoint.filePath == "" {
			endpoint.filePath = v
		}
		if v := StringVal(row, "file_language"); v != "" && endpoint.fileLanguage == "" {
			endpoint.fileLanguage = v
		}
		if v := StringVal(row, "repo_id"); v != "" && endpoint.repoID == "" {
			endpoint.repoID = v
		}
		if v := StringVal(row, "repo_name"); v != "" && endpoint.repoName == "" {
			endpoint.repoName = v
		}
		meta[uid] = endpoint
	}
}

// applyNornicDBEndpointMeta merges one endpoint's file/repo metadata onto a
// relationship row. File path and repository identity come only from
// enrichment, so they overwrite when present. Language prefers the entity node's
// own language (already projected by the core read) and falls back to the file
// language, matching the coalesce(node.language, file.language) the pre-split
// query expressed.
func applyNornicDBEndpointMeta(row map[string]any, prefix string, meta nornicDBRelationshipEndpointMeta) {
	if meta.filePath != "" {
		row[prefix+"_file_path"] = meta.filePath
	}
	if meta.repoID != "" {
		row[prefix+"_repo_id"] = meta.repoID
	}
	if meta.repoName != "" {
		row[prefix+"_repo_name"] = meta.repoName
	}
	if strings.TrimSpace(StringVal(row, prefix+"_language")) == "" && meta.fileLanguage != "" {
		row[prefix+"_language"] = meta.fileLanguage
	}
}

// nornicDBRelationshipFarEndpointPattern is the indexed entity anchor plus the
// relationship traversal to the far endpoints (targets for an outgoing read,
// sources for an incoming read). The relationship pattern carries the requested
// type constraint (for example `:INHERITS`); it is load-bearing for correctness
// — without it the enrichment would match endpoints reached by any relationship
// type, not just the type the core read returned — even though no relationship
// variable is bound or projected.
func nornicDBRelationshipFarEndpointPattern(direction string, relationshipType string, entityLabel string, entityIDProperty string) string {
	relPattern := nornicDBRelationshipPattern(relationshipType)
	entityPattern := nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	if direction == "incoming" {
		return entityPattern + `<-[` + relPattern + `]-(enrichNode)`
	}
	return entityPattern + `-[` + relPattern + `]->(enrichNode)`
}

// nornicDBRelationshipFarFileEnrichmentCypher reads the far endpoints' File
// metadata only, so a File without a REPO_CONTAINS edge still yields its path and
// language.
func nornicDBRelationshipFarFileEnrichmentCypher(direction string, relationshipType string, entityLabel string, entityIDProperty string) string {
	return `
		MATCH ` + nornicDBRelationshipFarEndpointPattern(direction, relationshipType, entityLabel, entityIDProperty) + `<-[:CONTAINS]-(enrichFile:File)
		RETURN coalesce(enrichNode.id, enrichNode.uid) as entity_uid,
		       enrichFile.relative_path as file_path,
		       enrichFile.language as file_language
		ORDER BY enrichNode.uid
		LIMIT $row_limit
	`
}

// nornicDBRelationshipFarRepoEnrichmentCypher reads the far endpoints' Repository
// metadata via the File that contains them.
func nornicDBRelationshipFarRepoEnrichmentCypher(direction string, relationshipType string, entityLabel string, entityIDProperty string) string {
	return `
		MATCH ` + nornicDBRelationshipFarEndpointPattern(direction, relationshipType, entityLabel, entityIDProperty) + `<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)
		RETURN coalesce(enrichNode.id, enrichNode.uid) as entity_uid,
		       enrichRepo.id as repo_id,
		       enrichRepo.name as repo_name
		ORDER BY enrichNode.uid
		LIMIT $row_limit
	`
}

// nornicDBRelationshipAnchorFileEnrichmentCypher reads the anchor entity's own
// File metadata (source for an outgoing read, target for an incoming read).
func nornicDBRelationshipAnchorFileEnrichmentCypher(entityLabel string, entityIDProperty string) string {
	entityPattern := nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	return `
		MATCH ` + entityPattern + `<-[:CONTAINS]-(enrichFile:File)
		RETURN coalesce(e.id, e.uid) as entity_uid,
		       enrichFile.relative_path as file_path,
		       enrichFile.language as file_language
		ORDER BY enrichFile.relative_path
		LIMIT $row_limit
	`
}

// nornicDBRelationshipAnchorRepoEnrichmentCypher reads the anchor entity's own
// Repository metadata via its containing File.
func nornicDBRelationshipAnchorRepoEnrichmentCypher(entityLabel string, entityIDProperty string) string {
	entityPattern := nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	return `
		MATCH ` + entityPattern + `<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)
		RETURN coalesce(e.id, e.uid) as entity_uid,
		       enrichRepo.id as repo_id,
		       enrichRepo.name as repo_name
		ORDER BY enrichFile.relative_path
		LIMIT $row_limit
	`
}
