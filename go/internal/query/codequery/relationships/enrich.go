// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// EndpointMeta is the file and repository metadata for one
// relationship endpoint, fetched by the OPTIONAL-MATCH-free enrichment
// reads and merged onto the relationship core rows in Go.
type EndpointMeta struct {
	FilePath     string
	FileLanguage string
	RepoID       string
	RepoName     string
}

// EnrichRows attaches file and repository metadata to the relationship
// core rows. The core read retains the split introduced for older
// NornicDB builds that corrupted function-call projections after
// OPTIONAL MATCH. The current v1.2.3 proof backend evaluates that shape
// correctly, but these index-anchored reads still preserve partial
// File-without-Repository metadata and bounded enrichment. Results are
// joined to the core rows by endpoint identity (coalesce(id, uid)).
//
// File and repository metadata are read as SEPARATE reads rather than
// one File->Repository path so that an endpoint with a File but no
// REPO_CONTAINS edge (a partially projected graph) still contributes
// its file path and language, exactly as the pre-split OPTIONAL MATCH
// clauses did -- a mandatory File->Repo path would drop the file
// metadata along with the absent repository.
//
// Enrichment is skipped entirely when the core rows carry no endpoint
// identity (the shape unit-test fakes produce), so those callers issue
// no extra query.
func EnrichRows(
	ctx context.Context,
	graph querycontract.GraphQuery,
	rows []map[string]any,
	entityID string,
	direction string,
	relationshipType string,
	entityLabel string,
	entityIDProperty string,
) ([]map[string]any, error) {
	meta := make(map[string]EndpointMeta)
	if haveEndpointUIDs(rows) {
		params := map[string]any{"entity_id": entityID, "row_limit": FetchLimit}
		for _, cypher := range []string{
			FarFileEnrichmentCypher(direction, relationshipType, entityLabel, entityIDProperty),
			FarRepoEnrichmentCypher(direction, relationshipType, entityLabel, entityIDProperty),
			AnchorFileEnrichmentCypher(entityLabel, entityIDProperty),
			AnchorRepoEnrichmentCypher(entityLabel, entityIDProperty),
		} {
			enrichRows, err := graph.Run(ctx, cypher, params)
			if err != nil {
				return nil, err
			}
			CollectEnrichment(meta, enrichRows)
		}
	}

	enriched := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		merged := querycontract.CloneAnyMap(row)
		applyEndpointMeta(merged, "source", meta[querycontract.StringVal(merged, "source_entity_uid")])
		applyEndpointMeta(merged, "target", meta[querycontract.StringVal(merged, "target_entity_uid")])
		delete(merged, "source_entity_uid")
		delete(merged, "target_entity_uid")
		enriched = append(enriched, merged)
	}
	return enriched, nil
}

func haveEndpointUIDs(rows []map[string]any) bool {
	for _, row := range rows {
		if querycontract.StringVal(row, "source_entity_uid") != "" || querycontract.StringVal(row, "target_entity_uid") != "" {
			return true
		}
	}
	return false
}

// CollectEnrichment folds enrichment rows into the identity-keyed
// metadata map, merging field by field. The file and repository reads
// each contribute only their own columns, so a File-only endpoint keeps
// its file metadata even though it never appears in the repository
// read. The first non-empty value per field wins, which keeps the merge
// deterministic even if a store carries duplicate File/Repository edges
// for a canonical entity.
func CollectEnrichment(meta map[string]EndpointMeta, rows []map[string]any) {
	for _, row := range rows {
		uid := querycontract.StringVal(row, "entity_uid")
		if uid == "" {
			continue
		}
		endpoint := meta[uid]
		if v := querycontract.StringVal(row, "file_path"); v != "" && endpoint.FilePath == "" {
			endpoint.FilePath = v
		}
		if v := querycontract.StringVal(row, "file_language"); v != "" && endpoint.FileLanguage == "" {
			endpoint.FileLanguage = v
		}
		if v := querycontract.StringVal(row, "repo_id"); v != "" && endpoint.RepoID == "" {
			endpoint.RepoID = v
		}
		if v := querycontract.StringVal(row, "repo_name"); v != "" && endpoint.RepoName == "" {
			endpoint.RepoName = v
		}
		meta[uid] = endpoint
	}
}

// applyEndpointMeta merges one endpoint's file/repo metadata onto a
// relationship row. File path and repository identity come only from
// enrichment, so they overwrite when present. Language prefers the
// entity node's own language (already projected by the core read) and
// falls back to the file language, matching the
// coalesce(node.language, file.language) the pre-split query expressed.
func applyEndpointMeta(row map[string]any, prefix string, meta EndpointMeta) {
	if meta.FilePath != "" {
		row[prefix+"_file_path"] = meta.FilePath
	}
	if meta.RepoID != "" {
		row[prefix+"_repo_id"] = meta.RepoID
	}
	if meta.RepoName != "" {
		row[prefix+"_repo_name"] = meta.RepoName
	}
	if strings.TrimSpace(querycontract.StringVal(row, prefix+"_language")) == "" && meta.FileLanguage != "" {
		row[prefix+"_language"] = meta.FileLanguage
	}
}

// farEndpointPattern is the indexed entity anchor plus the relationship
// traversal to the far endpoints (targets for an outgoing read, sources
// for an incoming read). The relationship pattern carries the requested
// type constraint (for example `:INHERITS`); it is load-bearing for
// correctness -- without it the enrichment would match endpoints
// reached by any relationship type, not just the type the core read
// returned -- even though no relationship variable is bound or
// projected.
func farEndpointPattern(direction string, relationshipType string, entityLabel string, entityIDProperty string) string {
	relPattern := NornicDBRelationshipPattern(relationshipType)
	entityPattern := NornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	if direction == "incoming" {
		return entityPattern + `<-[` + relPattern + `]-(enrichNode)`
	}
	return entityPattern + `-[` + relPattern + `]->(enrichNode)`
}

// FarFileEnrichmentCypher reads the far endpoints' File metadata only,
// so a File without a REPO_CONTAINS edge still yields its path and
// language.
func FarFileEnrichmentCypher(direction string, relationshipType string, entityLabel string, entityIDProperty string) string {
	return `
		MATCH ` + farEndpointPattern(direction, relationshipType, entityLabel, entityIDProperty) + `<-[:CONTAINS]-(enrichFile:File)
		RETURN coalesce(enrichNode.id, enrichNode.uid) as entity_uid,
		       enrichFile.relative_path as file_path,
		       enrichFile.language as file_language
		ORDER BY enrichNode.uid
		LIMIT $row_limit
	`
}

// FarRepoEnrichmentCypher reads the far endpoints' Repository metadata
// via the File that contains them.
func FarRepoEnrichmentCypher(direction string, relationshipType string, entityLabel string, entityIDProperty string) string {
	return `
		MATCH ` + farEndpointPattern(direction, relationshipType, entityLabel, entityIDProperty) + `<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)
		RETURN coalesce(enrichNode.id, enrichNode.uid) as entity_uid,
		       enrichRepo.id as repo_id,
		       enrichRepo.name as repo_name
		ORDER BY enrichNode.uid
		LIMIT $row_limit
	`
}

// AnchorFileEnrichmentCypher reads the anchor entity's own File
// metadata (source for an outgoing read, target for an incoming read).
func AnchorFileEnrichmentCypher(entityLabel string, entityIDProperty string) string {
	entityPattern := NornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	return `
		MATCH ` + entityPattern + `<-[:CONTAINS]-(enrichFile:File)
		RETURN coalesce(e.id, e.uid) as entity_uid,
		       enrichFile.relative_path as file_path,
		       enrichFile.language as file_language
		ORDER BY enrichFile.relative_path
		LIMIT $row_limit
	`
}

// AnchorRepoEnrichmentCypher reads the anchor entity's own Repository
// metadata via its containing File.
func AnchorRepoEnrichmentCypher(entityLabel string, entityIDProperty string) string {
	entityPattern := NornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	return `
		MATCH ` + entityPattern + `<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)
		RETURN coalesce(e.id, e.uid) as entity_uid,
		       enrichRepo.id as repo_id,
		       enrichRepo.name as repo_name
		ORDER BY enrichFile.relative_path
		LIMIT $row_limit
	`
}
