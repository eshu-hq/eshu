// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

var nornicDBRelationshipEntityLabels = []string{
	"Annotation", "Function", "Class", "Interface", "Module", "Variable",
	"Struct", "Enum", "Union", "Macro", "ImplBlock", "Typedef", "TypeAlias",
	"TypeAnnotation", "Component", "SqlColumn", "SqlFunction", "SqlIndex",
	"SqlMigration", "SqlTable", "SqlTrigger", "SqlView", "TerraformModule", "TerragruntConfig",
	"TerragruntDependency",
	// Flux typed entities (issue #5360 PR A; FluxHelmRelease/
	// FluxHelmRepository added issue #5483 C1): kept in lockstep with
	// graphLabelToContentEntityType so the graph-only relationship-label
	// fallback (h.Content nil) can resolve a Flux node's label, matching the
	// content-backed path's nornicDBGraphLabelForContentEntityType gate.
	"FluxKustomization", "FluxGitRepository", "FluxOCIRepository", "FluxBucket",
	"FluxHelmRelease", "FluxHelmRepository",
}

func (h *CodeHandler) nornicDBRelationshipEntityLabel(
	ctx context.Context,
	entityID string,
	repoID string,
) (string, error) {
	entityID = strings.TrimSpace(entityID)
	if h == nil || entityID == "" {
		return "", nil
	}
	if h.Content != nil {
		entity, err := h.Content.GetEntityContent(ctx, entityID)
		if err == nil && entity != nil {
			return nornicDBGraphLabelForContentEntityType(entity.EntityType), nil
		}
	}
	if h.Neo4j == nil {
		return "", nil
	}
	params := map[string]any{"entity_id": entityID}
	repoID = strings.TrimSpace(repoID)
	if repoID != "" {
		params["repo_id"] = repoID
	}
	for _, property := range []string{"uid", "id"} {
		rows, err := h.Neo4j.Run(
			ctx,
			nornicDBRelationshipEntityLabelCypher(property, repoID != ""),
			params,
		)
		if err != nil {
			return "", err
		}
		if len(rows) == 1 {
			return nornicDBPrimaryEntityLabel(rows[0]), nil
		}
		if len(rows) > 1 {
			return "", nil
		}
	}
	return "", nil
}

func nornicDBRelationshipEntityLabelCypher(property string, repositoryScoped bool) string {
	queries := make([]string, 0, len(nornicDBRelationshipEntityLabels))
	for _, label := range nornicDBRelationshipEntityLabels {
		match := fmt.Sprintf("MATCH (e:%s {%s: $entity_id})", label, property)
		if repositoryScoped {
			match += "<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(repo:Repository {id: $repo_id})"
		}
		queries = append(queries, fmt.Sprintf(
			"%s RETURN e.uid AS uid, e.id AS id, labels(e) AS labels",
			match,
		))
	}
	// Wrap the per-label UNION in CALL{} with a plain outer RETURN. A top-level
	// UNION is mis-parsed on the pinned NornicDB build (the branch columns are
	// mangled into a single row), while CALL{...UNION...} + a plain outer RETURN
	// executes correctly (#5287). Each branch keeps its single-label
	// inline-property anchor (the safe shape; a bare label-disjunction MATCH
	// matches zero rows on this build).
	return "CALL {\n" + strings.Join(queries, "\nUNION\n") + "\n}\nRETURN uid, id, labels\nLIMIT 2"
}

// nornicDBRelationshipMetadataPredicate builds the metadata lookup's WHERE and
// its parameters.
//
// The grant binds on the Repository alias rather than on the entity node,
// because this statement reaches the repository through two REQUIRED MATCH
// clauses -- an entity the graph cannot attribute to a repository never
// resolves here at all. #5167 batch 2b measured both halves live: the
// repository predicate in this clause position does decide row membership, and
// an entity with no File/Repository chain returns nothing.
//
// It is shared with POST /api/v0/code/relationships, which is still on the
// pending row-filtering ledger. Binding the grant here narrows that route for a
// scoped caller too, which is safe in the only direction that matters: the
// route stays fail-closed at the policy layer until it is promoted on its own
// proof.
func nornicDBRelationshipMetadataPredicate(
	name string,
	repoID string,
	access repositoryAccessFilter,
) (string, map[string]any) {
	params := make(map[string]any)
	var predicates []string
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		predicates = append(predicates, "e.name = $name")
		params["name"] = trimmed
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphCondition("repo"))
	}
	if trimmed := strings.TrimSpace(repoID); trimmed != "" {
		predicates = append(predicates, "repo.id = $repo_id")
		params["repo_id"] = trimmed
	}
	return strings.Join(predicates, " AND "), params
}

func nornicDBRelationshipMetadataCypher(predicate string, entityLabel string, entityIDProperty string) string {
	entityPattern := "(e" + nornicDBLabelPattern(entityLabel) + ")"
	if strings.TrimSpace(entityIDProperty) != "" {
		entityPattern = nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	}
	var predicates []string
	if trimmed := strings.TrimSpace(predicate); trimmed != "" {
		predicates = append(predicates, trimmed)
	}
	whereClause := ""
	if len(predicates) > 0 {
		whereClause = `
		WHERE ` + strings.Join(predicates, " AND ")
	}
	return `
		MATCH ` + entityPattern + `<-[:CONTAINS]-(f:File)
		MATCH (repo:Repository)-[:REPO_CONTAINS]->(f)
		` + whereClause + `
		RETURN coalesce(e.id, e.uid) as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		LIMIT 2
	`
}
