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
