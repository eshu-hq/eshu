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
	sourceRepoIDs []string,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, error) {
	if strings.TrimSpace(repoID) == "" || len(sourceRepoIDs) == 0 {
		return nil, nil
	}
	predicates := []string{
		"artifact.relationship_type = 'DEPLOYS_FROM'",
		"artifact.evidence_kind = 'FLUX_GIT_REPOSITORY_SOURCE'",
		"sourceRel.relationship_type = 'DEPLOYS_FROM'",
		"coalesce(artifact.flux_git_repository_name, '') <> ''",
		"coalesce(artifact.flux_git_repository_namespace, '') <> ''",
	}
	if access.scoped() {
		predicates = append(predicates, access.graphCondition("repo"))
	}
	cypher := `
		UNWIND $source_repo_ids AS source_id
		MATCH (repo:Repository {id: source_id})-[sourceRel:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
		WHERE ` + strings.Join(predicates, "\n\t\t  AND ") + `
		WITH repo, artifact
		LIMIT $source_limit
		MATCH (artifact)-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(targetRepo:Repository {id: $repo_id})
		WHERE targetRel.relationship_type = 'DEPLOYS_FROM'` + access.graphPredicate("targetRepo") + `
		RETURN repo.id AS source_id, targetRepo.id AS target_id,
		       artifact.flux_git_repository_namespace AS flux_git_repository_namespace,
		       artifact.flux_git_repository_name AS flux_git_repository_name
	`
	params := access.graphParams(map[string]any{"repo_id": repoID, "source_repo_ids": sourceRepoIDs, "source_limit": limit})
	return reader.Run(ctx, cypher, params)
}

func attachFluxDeploymentSourceTargetBindings(
	deploymentSources []map[string]any,
	bindings []map[string]any,
	saturated bool,
) []map[string]any {
	bindingsBySourceTarget := make(map[string]map[string]map[string]any, len(bindings))
	for _, binding := range bindings {
		sourceID := StringVal(binding, "source_id")
		targetID := StringVal(binding, "target_id")
		name := strings.TrimSpace(StringVal(binding, "flux_git_repository_name"))
		namespace := strings.TrimSpace(StringVal(binding, "flux_git_repository_namespace"))
		if sourceID == "" || targetID == "" || namespace == "" || name == "" {
			continue
		}
		key := sourceID + "\x00" + targetID
		if bindingsBySourceTarget[key] == nil {
			bindingsBySourceTarget[key] = make(map[string]map[string]any)
		}
		identity := namespace + "\x00" + name
		bindingsBySourceTarget[key][identity] = map[string]any{"namespace": namespace, "name": name}
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
		qualified := bindingsBySourceTarget[key]
		if len(qualified) == 0 {
			continue
		}
		identities := make([]string, 0, len(qualified))
		for identity := range qualified {
			identities = append(identities, identity)
		}
		sort.Strings(identities)
		values := make([]map[string]any, 0, len(identities))
		for _, identity := range identities {
			values = append(values, qualified[identity])
		}
		source["flux_git_repository_bindings"] = values
	}
	return deploymentSources
}
