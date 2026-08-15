// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The deployable_unit_edges family Odù (#5993, under the #5543 umbrella).
//
// Unlike sql_relationships/code_calls, this family's edges cannot be derived
// from an Odù's own facts through one pure fact-only extractor: a
// deployable-unit candidate's deployment_repo_id only exists once a
// RelDeploysFrom resolved relationship is applied
// (reducer.ExtractDeployableUnitCorrelationRows's doc comment), and that
// relationship is itself the output of the production evidence/resolution
// pipeline, not a hand-built value. So this Odù carries BOTH:
//
//   - a Dockerfile-backed app repository (repo-ifa-deployable-unit-app) that
//     gives ExtractWorkloadCandidates a workload signal, and
//   - a deployment/control repository (repo-ifa-deployable-unit-deploy) whose
//     ArgoCD Application file evidence (parsed_file_data.argocd_applications,
//     source_repo pointing at the app repository) is what
//     relationships.DiscoverEvidence + relationships.Resolve turn into the
//     RelDeploysFrom resolved relationship deployable_unit_edges materializes
//     on.
//
// The guard (materialized_edges_deployable_unit.go) runs those two real
// production seams over this Odù's facts before calling
// reducer.ExtractDeployableUnitCorrelationRows -- it never hand-authors a
// relationships.ResolvedRelationship.
const (
	deployableUnitFamilyOduName      = "odu:ifa-deployable-unit-family"
	deployableUnitFamilyGenerationID = "gen-ifa-deployable-unit-family-1"
	deployableUnitFamilyScopeID      = "scope-ifa-deployable-unit-family"

	// deployableUnitFamilyAppRepoID is the app repository whose deployable-unit
	// candidate is expected to admit.
	deployableUnitFamilyAppRepoID   = "repo-ifa-deployable-unit-app"
	deployableUnitFamilyAppRepoName = "deployable-unit-family"

	// deployableUnitFamilyDeployRepoID is the deployment/control repository
	// whose ArgoCD Application evidence names the app repository as its
	// deploy source.
	deployableUnitFamilyDeployRepoID   = "repo-ifa-deployable-unit-deploy"
	deployableUnitFamilyDeployRepoName = "deployable-unit-family-deploy"

	// deployableUnitFamilyArgoCDSourceRepoURL must contain
	// deployableUnitFamilyAppRepoName as a URL path segment: the alias matcher
	// relationships.DiscoverEvidence uses resolves an ArgoCD Application
	// source_repo URL to a catalog entry by matching that entry's derived
	// aliases (its repository fact's own name) as URL tokens, the same way
	// TestDiscoverStructuredArgoCDEvidencePreservesApplicationSourceTupleAlignment's
	// "helm-charts"/"config-repo" fixtures do.
	deployableUnitFamilyArgoCDSourceRepoURL = "https://github.com/ifa-org/deployable-unit-family.git"
)

// deployableUnitFamilyOdu returns the binary-portable catalog representation
// of the deployable_unit_edges family fixture.
func deployableUnitFamilyOdu() CatalogOdu {
	sourceRunID := "run-ifa-deployable-unit-family-1"
	factsForOdu := []facts.Envelope{
		deployableUnitCatalogRepositoryFact(codegraphv1.Repository{
			RepoID:      deployableUnitFamilyAppRepoID,
			SourceRunID: &sourceRunID,
			GraphID:     strPtr(deployableUnitFamilyAppRepoID),
			Name:        strPtr(deployableUnitFamilyAppRepoName),
		}),
		deployableUnitCatalogFileFact(codegraphv1.File{
			RepoID: deployableUnitFamilyAppRepoID, RelativePath: "Dockerfile",
			Language: strPtr("dockerfile"),
			ParsedFileData: map[string]any{
				"dockerfile_stages": []any{
					map[string]any{"name": "runtime"},
				},
			},
		}),
		deployableUnitCatalogRepositoryFact(codegraphv1.Repository{
			RepoID:      deployableUnitFamilyDeployRepoID,
			SourceRunID: &sourceRunID,
			GraphID:     strPtr(deployableUnitFamilyDeployRepoID),
			Name:        strPtr(deployableUnitFamilyDeployRepoName),
		}),
		deployableUnitCatalogFileFact(codegraphv1.File{
			RepoID: deployableUnitFamilyDeployRepoID, RelativePath: "apps/deployable-unit-app.yaml",
			ParsedFileData: map[string]any{
				"argocd_applications": []any{
					map[string]any{
						"name":           "ifa-deployable-unit-app",
						"source_repo":    deployableUnitFamilyArgoCDSourceRepoURL,
						"dest_namespace": "prod",
					},
				},
			},
		}),
	}
	return CatalogOdu{
		Odu:    Odu{Name: deployableUnitFamilyOduName, Facts: factsForOdu},
		Detail: "one Dockerfile-backed app repository plus one ArgoCD-deploying control repository, proving CORRELATES_DEPLOYABLE_UNIT through the real evidence-discovery and resolution seams, not a hand-authored resolved relationship",
	}
}

func deployableUnitCatalogFact(kind, stableKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID: deployableUnitFamilyScopeID, GenerationID: deployableUnitFamilyGenerationID, FactKind: kind,
		StableFactKey: stableKey, SchemaVersion: "1.0.0", CollectorKind: "git",
		SourceConfidence: "observed", Payload: payload,
	}
}

func deployableUnitCatalogRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode deployable-unit catalog repository %q: %v", repository.RepoID, err))
	}
	return deployableUnitCatalogFact(factschema.FactKindCodegraphRepository, "repository:"+repository.RepoID, payload)
}

func deployableUnitCatalogFileFact(file codegraphv1.File) facts.Envelope {
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode deployable-unit catalog file %q: %v", file.RelativePath, err))
	}
	return deployableUnitCatalogFact(factschema.FactKindCodegraphFile, "file:"+file.RepoID+":"+file.RelativePath, payload)
}

func strPtr(value string) *string {
	return &value
}
