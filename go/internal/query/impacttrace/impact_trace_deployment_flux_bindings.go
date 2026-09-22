// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type fluxDeploymentSourceTargetBindingResult struct {
	rows              []map[string]any
	firstHopCount     int
	firstHopSaturated bool
}

// Rows returns the expanded target-binding rows. FirstHopCount and
// FirstHopSaturated report the fanned-out first-hop observation count and
// whether it hit the sentinel. The accessors let the impact/ subpackage
// consume the result without naming its unexported type. See #6060.
func (r fluxDeploymentSourceTargetBindingResult) Rows() []map[string]any { return r.rows }

// FirstHopCount reports how many first-hop rows the fan-out observed.
func (r fluxDeploymentSourceTargetBindingResult) FirstHopCount() int { return r.firstHopCount }

// FirstHopSaturated reports whether the first-hop fan-out hit the sentinel.
func (r fluxDeploymentSourceTargetBindingResult) FirstHopSaturated() bool {
	return r.firstHopSaturated
}

func FetchFluxDeploymentSourceTargetBindings(
	ctx context.Context,
	reader querycontract.GraphQuery,
	repoID string,
	sourceRepoIDs []string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (fluxDeploymentSourceTargetBindingResult, error) {
	if strings.TrimSpace(repoID) == "" || len(sourceRepoIDs) == 0 || limit <= 0 {
		return fluxDeploymentSourceTargetBindingResult{}, nil
	}
	predicates := []string{
		"artifact.relationship_type = 'DEPLOYS_FROM'",
		"artifact.evidence_kind = 'FLUX_GIT_REPOSITORY_SOURCE'",
		"sourceRel.relationship_type = 'DEPLOYS_FROM'",
		"coalesce(artifact.flux_git_repository_name, '') <> ''",
		"coalesce(artifact.flux_git_repository_namespace, '') <> ''",
	}
	if access.Scoped() {
		predicates = append(predicates, access.GraphCondition("repo"))
	}
	firstHopCypher := `
		UNWIND $source_repo_ids AS source_id
		MATCH (repo:Repository {id: source_id})-[sourceRel:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
		WHERE ` + strings.Join(predicates, "\n\t\t  AND ") + `
		RETURN repo.id AS source_id, artifact.id AS artifact_id,
		       artifact.flux_git_repository_namespace AS flux_git_repository_namespace,
		       artifact.flux_git_repository_name AS flux_git_repository_name
		LIMIT $source_limit
	`
	params := access.GraphParams(map[string]any{"repo_id": repoID, "source_repo_ids": sourceRepoIDs, "source_limit": limit})
	firstHopRows, err := reader.Run(ctx, firstHopCypher, params)
	if err != nil {
		return fluxDeploymentSourceTargetBindingResult{}, err
	}
	result := fluxDeploymentSourceTargetBindingResult{firstHopCount: len(firstHopRows)}
	if len(firstHopRows) >= limit {
		result.firstHopSaturated = true
		return result, nil
	}
	artifactIDs := make([]string, 0, len(firstHopRows))
	for _, row := range firstHopRows {
		if artifactID := querycontract.StringVal(row, "artifact_id"); artifactID != "" {
			artifactIDs = append(artifactIDs, artifactID)
		}
	}
	artifactIDs = querycontract.UniqueSortedStrings(artifactIDs)
	if len(artifactIDs) == 0 {
		return result, nil
	}

	expansionCypher := `
		UNWIND $artifact_ids AS artifact_id
		MATCH (artifact:EvidenceArtifact {id: artifact_id})<-[sourceRel:HAS_DEPLOYMENT_EVIDENCE]-(repo:Repository)
		MATCH (artifact)-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(targetRepo:Repository {id: $repo_id})
		WHERE sourceRel.relationship_type = 'DEPLOYS_FROM'
		  AND targetRel.relationship_type = 'DEPLOYS_FROM'
		  AND repo.id IN $source_repo_ids` + access.GraphPredicate("repo") + access.GraphPredicate("targetRepo") + `
		RETURN repo.id AS source_id, targetRepo.id AS target_id,
		       artifact.flux_git_repository_namespace AS flux_git_repository_namespace,
		       artifact.flux_git_repository_name AS flux_git_repository_name
	`
	params["artifact_ids"] = artifactIDs
	result.rows, err = reader.Run(ctx, expansionCypher, params)
	return result, err
}

func AttachFluxDeploymentSourceTargetBindings(
	deploymentSources []map[string]any,
	bindings []map[string]any,
	saturated bool,
) []map[string]any {
	bindingsBySourceTarget := make(map[string]map[string]map[string]any, len(bindings))
	for _, binding := range bindings {
		sourceID := querycontract.StringVal(binding, "source_id")
		targetID := querycontract.StringVal(binding, "target_id")
		name := strings.TrimSpace(querycontract.StringVal(binding, "flux_git_repository_name"))
		namespace := strings.TrimSpace(querycontract.StringVal(binding, "flux_git_repository_namespace"))
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
		if querycontract.StringVal(source, "relationship_type") != "DEPLOYS_FROM" {
			continue
		}
		if saturated {
			source["flux_target_bindings_saturated"] = true
			continue
		}
		key := querycontract.StringVal(source, "source_id") + "\x00" + querycontract.StringVal(source, "target_id")
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
