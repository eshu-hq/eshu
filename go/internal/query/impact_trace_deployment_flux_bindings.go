// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strings"
)

func fetchFluxDeploymentSourceTargetBindings(
	ctx context.Context,
	reader GraphQuery,
	repoID string,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, error) {
	if strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	scopeClause := access.graphWhereClause("repo")
	if access.scoped() {
		scopeClause += " AND " + access.graphCondition("targetRepo")
	}
	cypher := `
		MATCH (targetRepo:Repository {id: $repo_id})<-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact:EvidenceArtifact)<-[sourceRel:HAS_DEPLOYMENT_EVIDENCE]-(repo:Repository)
		` + scopeClause + `
		WHERE artifact.relationship_type = 'DEPLOYS_FROM'
		  AND artifact.evidence_kind = 'FLUX_GIT_REPOSITORY_SOURCE'
		  AND targetRel.relationship_type = 'DEPLOYS_FROM'
		  AND sourceRel.relationship_type = 'DEPLOYS_FROM'
		  AND coalesce(artifact.matched_alias, '') <> ''
		RETURN DISTINCT repo.id AS source_id, targetRepo.id AS target_id,
		       artifact.matched_alias AS flux_git_repository_name
		ORDER BY source_id, target_id, flux_git_repository_name
		LIMIT $source_limit
	`
	params := access.graphParams(map[string]any{"repo_id": repoID, "source_limit": limit})
	return reader.Run(ctx, cypher, params)
}

func attachFluxDeploymentSourceTargetBindings(
	deploymentSources []map[string]any,
	bindings []map[string]any,
	saturated bool,
) []map[string]any {
	namesBySourceTarget := make(map[string]map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		sourceID := StringVal(binding, "source_id")
		targetID := StringVal(binding, "target_id")
		name := strings.TrimSpace(StringVal(binding, "flux_git_repository_name"))
		if sourceID == "" || targetID == "" || name == "" {
			continue
		}
		key := sourceID + "\x00" + targetID
		if namesBySourceTarget[key] == nil {
			namesBySourceTarget[key] = make(map[string]struct{})
		}
		namesBySourceTarget[key][name] = struct{}{}
	}
	for _, source := range deploymentSources {
		if StringVal(source, "relationship_type") != "DEPLOYS_FROM" {
			continue
		}
		if saturated {
			source["flux_target_bindings_saturated"] = true
			continue
		}
		key := StringVal(source, "source_id") + "\x00" + StringVal(source, "target_id")
		names := namesBySourceTarget[key]
		if len(names) == 0 {
			continue
		}
		values := make([]string, 0, len(names))
		for name := range names {
			values = append(values, name)
		}
		sort.Strings(values)
		source["flux_git_repository_names"] = values
	}
	return deploymentSources
}
