// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The workload_dependency family Odù (#6003, under the #5543 umbrella).
//
// Unlike repo_dependency, this family's single DEPENDS_ON edge type has TWO
// production seams, not one: a repo-to-repo dependency edge
// (reducer.RepoDependencyEdge) resolves the SAME way repo_dependency's own
// edges do -- DiscoveredEvidence -> relationships.Resolve, filtered to the
// rows whose relationship_type routes to DEPENDS_ON -- but that pair only
// becomes a WORKLOAD dependency once each endpoint repository also owns
// exactly one materialized workload
// (reducer.ExtractWorkloadCandidates -> reducer.BuildProjectionRowsWithInfrastructurePlatforms)
// and reducer.ReconcileWorkloadDependencyEdges's two independent drop
// conditions (workload_dependency_reconciliation.go:137-146) do not fire. See
// materialized_edges_workload_dependency.go for how this Odù's own facts
// drive both seams and the fake reducer.WorkloadDependencyGraphLookup that
// closes the loop.
//
// Three repo-to-repo DEPENDS_ON pairs, all discovered from a Docker Compose
// `depends_on:` content fact -- the exact evidence shape
// repo_dependency_family_catalog.go's positive DEPENDS_ON case already
// proves at the evidence layer:
//
//   - positive: sourceRepo depends on targetRepo. Both carry a Kubernetes
//     Deployment file fact, so ExtractWorkloadCandidates admits exactly one
//     workload per repo and the pair is expected to materialize as one
//     Workload->Workload DEPENDS_ON edge.
//   - multi-workload: multiSourceRepo depends on multiTargetRepo. Both also
//     carry a Deployment file fact and are current-generation repos, but the
//     guard's fake graph lookup additionally reports multiTargetRepo as
//     already owning a SECOND, unrelated workload -- proving
//     ReconcileWorkloadDependencyEdges' `len(targetWorkloads) != 1` drop
//     (workload_dependency_reconciliation.go:138) actually fires, not merely
//     that the expected set happens to omit this pair.
//   - orphan: orphanSourceRepo depends on orphanTargetRepo, but NEITHER
//     carries any Kubernetes/ArgoCD signal, so neither produces a workload
//     candidate and neither is a "current" repo
//     (reducer.RepoDescriptor). The guard's fake graph lookup reports one
//     PERSISTED workload for each anyway (simulating a stale prior
//     generation), satisfying the `len(...) == 1` checks so the pair reaches
//     the ownership check -- proving the `neither repo is current` drop
//     (workload_dependency_reconciliation.go:141-146) fires independently of
//     the multi-workload drop.
const (
	workloadDependencyFamilyOduName      = "odu:ifa-workload-dependency-family"
	workloadDependencyFamilyScopeID      = "scope-ifa-workload-dependency-family"
	workloadDependencyFamilyGenerationID = "gen-ifa-workload-dependency-family-1"
	workloadDependencyFamilySourceRunID  = "run-ifa-workload-dependency-family-1"
	workloadDependencyFamilyCommitSHA    = "fedcba9876543210fedcba9876543210fedcba9"

	workloadDependencyFamilySourceRepoID = "repo-ifa-workload-dependency-source"
	workloadDependencyFamilySourceName   = "workload-dependency-source"

	workloadDependencyFamilyTargetRepoID = "repo-ifa-workload-dependency-target"
	workloadDependencyFamilyTargetName   = "workload-dependency-target"

	workloadDependencyFamilyMultiSourceRepoID = "repo-ifa-workload-dependency-multi-source"
	workloadDependencyFamilyMultiSourceName   = "workload-dependency-multi-source"

	workloadDependencyFamilyMultiTargetRepoID = "repo-ifa-workload-dependency-multi-target"
	workloadDependencyFamilyMultiTargetName   = "workload-dependency-multi-target"

	workloadDependencyFamilyOrphanSourceRepoID = "repo-ifa-workload-dependency-orphan-source"
	workloadDependencyFamilyOrphanSourceName   = "workload-dependency-orphan-source"

	workloadDependencyFamilyOrphanTargetRepoID = "repo-ifa-workload-dependency-orphan-target"
	workloadDependencyFamilyOrphanTargetName   = "workload-dependency-orphan-target"

	// workloadDependencyFamilyMultiTargetPhantomWorkloadID is the SECOND
	// workload the guard's fake lookup reports multiTargetRepo already owns,
	// on top of the one real candidate ExtractWorkloadCandidates admits from
	// this Odù's own Deployment file fact. It never appears as a
	// RepoDescriptor or a WorkloadCandidate -- it exists only in the fake
	// lookup's ListRepoWorkloads response -- so `len(targetWorkloads) != 1`
	// fires for a reason entirely outside this Odù's own facts, the same way
	// a live graph's stale second workload record would.
	workloadDependencyFamilyMultiTargetPhantomWorkloadID = "workload:workload-dependency-multi-target-phantom"

	// workloadDependencyFamilyOrphanSourcePersistedWorkloadID and
	// workloadDependencyFamilyOrphanTargetPersistedWorkloadID are the single
	// PERSISTED workload the fake lookup reports for each orphan repo, even
	// though neither repo produces a current-generation WorkloadCandidate.
	// This satisfies ReconcileWorkloadDependencyEdges'
	// `len(sourceWorkloads) == 1 && len(targetWorkloads) == 1` check so the
	// pair reaches the SEPARATE `neither repo is current` ownership check
	// (workload_dependency_reconciliation.go:141-146), isolating that drop
	// reason from the multi-workload one above.
	workloadDependencyFamilyOrphanSourcePersistedWorkloadID = "workload:workload-dependency-orphan-source-persisted"
	workloadDependencyFamilyOrphanTargetPersistedWorkloadID = "workload:workload-dependency-orphan-target-persisted"
)

// workloadDependencyFamilyOdu returns the binary-portable catalog
// representation of the workload_dependency family fixture.
func workloadDependencyFamilyOdu() CatalogOdu {
	factsForOdu := []facts.Envelope{
		workloadDependencyFamilyRepositoryFact(workloadDependencyFamilySourceRepoID, workloadDependencyFamilySourceName),
		workloadDependencyFamilyRepositoryFact(workloadDependencyFamilyTargetRepoID, workloadDependencyFamilyTargetName),
		workloadDependencyFamilyRepositoryFact(workloadDependencyFamilyMultiSourceRepoID, workloadDependencyFamilyMultiSourceName),
		workloadDependencyFamilyRepositoryFact(workloadDependencyFamilyMultiTargetRepoID, workloadDependencyFamilyMultiTargetName),
		workloadDependencyFamilyRepositoryFact(workloadDependencyFamilyOrphanSourceRepoID, workloadDependencyFamilyOrphanSourceName),
		workloadDependencyFamilyRepositoryFact(workloadDependencyFamilyOrphanTargetRepoID, workloadDependencyFamilyOrphanTargetName),

		// Repo-to-repo DEPENDS_ON evidence, one Docker Compose `depends_on:`
		// content fact per pair, mirroring
		// repo_dependency_family_catalog.go's positive DEPENDS_ON case exactly
		// (same artifact_type/body shape).
		workloadDependencyFamilyDependsOnContentFact(workloadDependencyFamilySourceRepoID, "deploy/docker-compose.yml", workloadDependencyFamilyTargetName),
		workloadDependencyFamilyDependsOnContentFact(workloadDependencyFamilyMultiSourceRepoID, "deploy/docker-compose.yml", workloadDependencyFamilyMultiTargetName),
		workloadDependencyFamilyDependsOnContentFact(workloadDependencyFamilyOrphanSourceRepoID, "deploy/docker-compose.yml", workloadDependencyFamilyOrphanTargetName),

		// Kubernetes Deployment file facts: the positive and multi-workload
		// pairs each get one per repo, so ExtractWorkloadCandidates admits
		// exactly one workload for each of those four repos. The orphan pair
		// gets none, so neither orphan repo produces a candidate at all.
		workloadDependencyFamilyK8sDeploymentFact(workloadDependencyFamilySourceRepoID, workloadDependencyFamilySourceName),
		workloadDependencyFamilyK8sDeploymentFact(workloadDependencyFamilyTargetRepoID, workloadDependencyFamilyTargetName),
		workloadDependencyFamilyK8sDeploymentFact(workloadDependencyFamilyMultiSourceRepoID, workloadDependencyFamilyMultiSourceName),
		workloadDependencyFamilyK8sDeploymentFact(workloadDependencyFamilyMultiTargetRepoID, workloadDependencyFamilyMultiTargetName),
	}
	return CatalogOdu{
		Odu: Odu{Name: workloadDependencyFamilyOduName, Facts: factsForOdu},
		Detail: "three repo-to-repo DEPENDS_ON pairs from Docker Compose depends_on evidence: one positive pair whose two repos each own exactly one current-generation Kubernetes-Deployment workload " +
			"(expected to materialize as one Workload->Workload DEPENDS_ON edge), one pair whose target repo the fake graph lookup reports as owning a second, unrelated workload (must be dropped), " +
			"and one pair with no Kubernetes evidence on either repo, so neither is a current-generation repo even though the fake graph lookup reports a persisted workload for each (must also be dropped) -- " +
			"proving DiscoveredEvidence -> relationships.Resolve (for the repo edge) plus ExtractWorkloadCandidates -> BuildProjectionRowsWithInfrastructurePlatforms (for the workload map) feed the real " +
			"reducer.ReconcileWorkloadDependencyEdges and its two independent drop reasons both fire for the reasons this Odù's own facts (and the fake lookup) provide, not merely that the expected set omits them.",
	}
}

// workloadDependencyFamilyRepositoryFact builds one raw "repository" fact
// carrying BOTH the repo_id/name/repo_slug fields
// relationships.RepositoryCatalogEntry's catalog matcher reads (mirroring
// repoDependencyFamilyRepositoryFact) AND the graph_id field
// reducer.ExtractWorkloadCandidates reads (candidate_loader.go's Pass 1) --
// this family needs both production seams to resolve the SAME repository
// identity from one fact, unlike repo_dependency which only needs the
// former.
func workloadDependencyFamilyRepositoryFact(repoID, name string) facts.Envelope {
	return workloadDependencyFamilyFact(repositoryFactKind, "repository:"+repoID, map[string]any{
		"name":          name,
		"repo_id":       repoID,
		"graph_id":      repoID,
		"repo_slug":     "ifa-org/" + name,
		"source_run_id": workloadDependencyFamilySourceRunID,
	})
}

// workloadDependencyFamilyDependsOnContentFact builds one raw "content" fact
// in the git-content collector's content_path/content_body shape, carrying a
// Docker Compose service `depends_on:` entry naming targetName -- the same
// evidence relationships.EvidenceKindDockerComposeDependsOn discovers and
// resolves to a DEPENDS_ON relationship
// (repo_dependency_family_catalog.go's positive DEPENDS_ON case).
func workloadDependencyFamilyDependsOnContentFact(sourceRepoID, path, targetName string) facts.Envelope {
	return workloadDependencyFamilyFact(contentFactKind, "content:"+sourceRepoID+":"+path, map[string]any{
		"artifact_type": "docker_compose",
		"commit_sha":    workloadDependencyFamilyCommitSHA,
		"content_body":  "services:\n  app:\n    depends_on:\n      - " + targetName + "\n",
		"content_path":  path,
		"repo_id":       sourceRepoID,
	})
}

// workloadDependencyFamilyK8sDeploymentFact builds one raw "file" fact
// carrying a parsed Kubernetes Deployment resource, in the
// parsed_file_data.k8s_resources shape
// TestExtractWorkloadCandidatesFromK8sResourceFacts
// (go/internal/reducer/candidate_loader_test.go) already proves
// reducer.ExtractWorkloadCandidates admits into exactly one WorkloadCandidate
// per repo, at confidence 0.98 (DefaultWorkloadSignalConfidence, well above
// workloadMaterializationMinConfidence's 0.82 floor) and classification
// "service" (InferWorkloadClassification's deployment/service/statefulset/
// daemonset resource-kind arm), both required for
// BuildProjectionRowsWithInfrastructurePlatforms to admit a RepoDescriptor.
func workloadDependencyFamilyK8sDeploymentFact(repoID, repoName string) facts.Envelope {
	return workloadDependencyFamilyFact(fileFactKind, "file:"+repoID+":deploy/deployment.yaml", map[string]any{
		"repo_id":       repoID,
		"relative_path": "deploy/deployment.yaml",
		"parsed_file_data": map[string]any{
			"k8s_resources": []any{
				map[string]any{
					"name":      repoName,
					"kind":      "Deployment",
					"namespace": "production",
				},
			},
		},
	})
}

// workloadDependencyFamilyFact stamps the family's single scope/generation
// onto one fact envelope, mirroring repoDependencyFamilyFact's field set
// exactly (no FactID, ObservedAt, or SourceRef) so the compiled Odù and its
// cassette-loaded projection render identical zero-value envelopes on those
// fields for TestWorkloadDependencyFamilyCassetteMatchesCompiledCatalog's
// reflect.DeepEqual to hold.
func workloadDependencyFamilyFact(kind, stableKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID: workloadDependencyFamilyScopeID, GenerationID: workloadDependencyFamilyGenerationID, FactKind: kind,
		StableFactKey: stableKey, SchemaVersion: "1.0.0", CollectorKind: "git",
		SourceConfidence: "observed", Payload: payload,
	}
}
