// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"path/filepath"

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

	// deployableUnitFamilyRejectedRepoID carries a Dockerfile and NOTHING
	// else -- the exact fixture shape
	// TestDeployableUnitCorrelationHandleRejectsDockerfileOnlyCandidate proves
	// stays below DockerfileRulePack's 0.90 MinAdmissionConfidence. Its row
	// exists (repo_id and admission_state both populate) but admission_state
	// never becomes "admitted", proving AdmittedDeployableUnitRows' first drop
	// condition: a candidate can reach row construction and still be
	// correctly excluded from the materialized set.
	deployableUnitFamilyRejectedRepoID   = "repo-ifa-deployable-unit-rejected"
	deployableUnitFamilyRejectedRepoName = "deployable-unit-family-rejected"

	// deployableUnitFamilyAdmittedNoDeployRepoID carries a Dockerfile PLUS a
	// Jenkinsfile -- the exact fixture shape
	// TestDeployableUnitCorrelationHandleAdmitsJenkinsBackedServiceCandidate
	// proves reaches JenkinsRulePack's 0.84 MinAdmissionConfidence on
	// structural evidence ALONE, with no RelDeploysFrom resolved relationship
	// involved at all. Its candidate is admitted (admission_state ==
	// "admitted") but deployment_repo_id stays "" because nothing in this
	// Odù's evidence names a deployment repository for it. This proves
	// AdmittedDeployableUnitRows' SECOND, independent drop condition: an
	// admitted row can still be correctly excluded from the materialized set
	// for lacking a deployment_repo_id -- a fixture of only-admitted rows with
	// a deployment_repo_id cannot distinguish that filter from a no-op.
	deployableUnitFamilyAdmittedNoDeployRepoID   = "repo-ifa-deployable-unit-jenkins"
	deployableUnitFamilyAdmittedNoDeployRepoName = "deployable-unit-family-jenkins"
)

// deployableUnitFamilyOdu returns the binary-portable catalog representation
// of the deployable_unit_edges family fixture.
func deployableUnitFamilyOdu() CatalogOdu {
	sourceRunID := "run-ifa-deployable-unit-family-1"
	// LocalPath values are distinct from every other cataloged family's
	// (SQLFamilyLocalPath "/repo", code_calls' "/repo-code-calls") so the
	// live determinism matrix's canonical path cleanup for one family cannot
	// delete this family's repository, mirroring
	// TestCodeCallFamilyRepositoryIdentityDoesNotCollideWithSQLFamily's proof.
	appLocalPath := "/repo-ifa-deployable-unit-app"
	deployLocalPath := "/repo-ifa-deployable-unit-deploy"
	rejectedLocalPath := "/repo-ifa-deployable-unit-rejected"
	jenkinsLocalPath := "/repo-ifa-deployable-unit-jenkins"
	factsForOdu := []facts.Envelope{
		deployableUnitCatalogRepositoryFact(codegraphv1.Repository{
			RepoID:      deployableUnitFamilyAppRepoID,
			SourceRunID: &sourceRunID,
			LocalPath:   &appLocalPath,
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
		deployableUnitCatalogFollowupFact("deployable_unit_correlation", "repo:", deployableUnitFamilyAppRepoID, appLocalPath),
		deployableUnitCatalogFollowupFact("deployment_mapping", "deployment:", deployableUnitFamilyAppRepoID, appLocalPath),
		deployableUnitCatalogRepositoryFact(codegraphv1.Repository{
			RepoID:      deployableUnitFamilyDeployRepoID,
			SourceRunID: &sourceRunID,
			LocalPath:   &deployLocalPath,
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
		deployableUnitCatalogFollowupFact("deployable_unit_correlation", "repo:", deployableUnitFamilyDeployRepoID, deployLocalPath),
		deployableUnitCatalogFollowupFact("deployment_mapping", "deployment:", deployableUnitFamilyDeployRepoID, deployLocalPath),
		// Negative case 1: Dockerfile-only, no other evidence. Reaches row
		// construction but stays rejected (admission_state != "admitted").
		deployableUnitCatalogRepositoryFact(codegraphv1.Repository{
			RepoID:      deployableUnitFamilyRejectedRepoID,
			SourceRunID: &sourceRunID,
			LocalPath:   &rejectedLocalPath,
			GraphID:     strPtr(deployableUnitFamilyRejectedRepoID),
			Name:        strPtr(deployableUnitFamilyRejectedRepoName),
		}),
		deployableUnitCatalogFileFact(codegraphv1.File{
			RepoID: deployableUnitFamilyRejectedRepoID, RelativePath: "Dockerfile",
			Language: strPtr("dockerfile"),
			ParsedFileData: map[string]any{
				"dockerfile_stages": []any{
					map[string]any{"name": "runtime"},
				},
			},
		}),
		deployableUnitCatalogFollowupFact("deployable_unit_correlation", "repo:", deployableUnitFamilyRejectedRepoID, rejectedLocalPath),
		deployableUnitCatalogFollowupFact("deployment_mapping", "deployment:", deployableUnitFamilyRejectedRepoID, rejectedLocalPath),
		// Negative case 2: Dockerfile + Jenkinsfile, admitted via
		// JenkinsRulePack on structural evidence alone, but with no
		// RelDeploysFrom evidence naming a deployment repository --
		// admission_state == "admitted" AND deployment_repo_id == "".
		deployableUnitCatalogRepositoryFact(codegraphv1.Repository{
			RepoID:      deployableUnitFamilyAdmittedNoDeployRepoID,
			SourceRunID: &sourceRunID,
			LocalPath:   &jenkinsLocalPath,
			GraphID:     strPtr(deployableUnitFamilyAdmittedNoDeployRepoID),
			Name:        strPtr(deployableUnitFamilyAdmittedNoDeployRepoName),
		}),
		deployableUnitCatalogFileFact(codegraphv1.File{
			RepoID: deployableUnitFamilyAdmittedNoDeployRepoID, RelativePath: "Dockerfile",
			Language: strPtr("dockerfile"),
			ParsedFileData: map[string]any{
				"dockerfile_stages": []any{
					map[string]any{"name": "runtime"},
				},
			},
		}),
		deployableUnitCatalogFileFact(codegraphv1.File{
			RepoID: deployableUnitFamilyAdmittedNoDeployRepoID, RelativePath: "Jenkinsfile",
			Language: strPtr("groovy"),
			ParsedFileData: map[string]any{
				"jenkins_pipeline_calls": []any{"deployShared"},
			},
		}),
		deployableUnitCatalogFollowupFact("deployable_unit_correlation", "repo:", deployableUnitFamilyAdmittedNoDeployRepoID, jenkinsLocalPath),
		deployableUnitCatalogFollowupFact("deployment_mapping", "deployment:", deployableUnitFamilyAdmittedNoDeployRepoID, jenkinsLocalPath),
	}
	return CatalogOdu{
		Odu: Odu{Name: deployableUnitFamilyOduName, Facts: factsForOdu},
		Detail: "one Dockerfile-backed app repository plus one ArgoCD-deploying control repository (positive case: admitted with a deployment_repo_id), " +
			"one Dockerfile-only repository (negative case: rejected), and one Dockerfile+Jenkinsfile repository (negative case: admitted but no deployment_repo_id) -- " +
			"proving CORRELATES_DEPLOYABLE_UNIT through the real evidence-discovery and resolution seams, and proving both of AdmittedDeployableUnitRows' independent drop conditions",
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

// deployableUnitCatalogFollowupFact reproduces one "shared_followup" fact the
// git collector emits unconditionally for every repository it snapshots
// (go/internal/collector/git_fact_builder.go:417-422 calls
// workloadIdentityFactEnvelope, deployableUnitCorrelationFactEnvelope, and
// deploymentMappingFactEnvelope for every repo with no evidence-content gate).
// Without this fact for a repo's reducerDomain, the projector never enqueues
// that domain's reducer intent for the repo at all -- a live cassette that
// omits it proves nothing, regardless of what evidence the repo's other
// facts carry. entityKeyPrefix matches the two production followups this
// family depends on: "repo:" for deployable_unit_correlation
// (deployableUnitCorrelationFactEnvelope) and "deployment:" for
// deployment_mapping (deploymentMappingFactEnvelope), both keyed on
// filepath.Base(repoPath) exactly as the collector does.
func deployableUnitCatalogFollowupFact(reducerDomain, entityKeyPrefix, repoID, localPath string) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": reducerDomain,
		"entity_key":     entityKeyPrefix + filepath.Base(localPath),
		"reason":         "repository snapshot emitted " + reducerDomain + " follow-up",
		"repo_id":        repoID,
	}
	return deployableUnitCatalogFact("shared_followup", "shared_followup:"+repoID+":"+reducerDomain, payload)
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
