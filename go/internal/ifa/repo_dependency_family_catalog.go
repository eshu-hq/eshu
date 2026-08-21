// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The repo_dependency family Odù (#5999, under the #5543 umbrella).
//
// Unlike deployable_unit_edges, six of this family's relationship types
// (DEPENDS_ON, DEPLOYS_FROM, DISCOVERS_CONFIG_IN,
// PROVISIONS_DEPENDENCY_FOR, USES_MODULE, READS_CONFIG_FROM -- the
// alternation cypher.RepoDependencyMaterializedEdgeTypes splits out of
// repoDependencyRelationshipEdgeTypes) derive from facts ALONE: each is a
// relationships.EvidenceFact the production discovery layer
// (relationships.DiscoverEvidence) emits directly off one raw "content"
// fact's artifact_type/body, with no resolved-relationship precondition the
// way deployable_unit_edges' deployment_repo_id has. So this Odù needs no
// second production seam beyond DiscoveredEvidence -> relationships.Resolve
// (see materialized_edges_repo_dependency.go). The seventh type, RUNS_ON,
// derives from an ArgoCD Application destination in a typed file fact. That
// same fact carries a Kubernetes Deployment in namespace prod, so the
// production workload extraction/projection seams derive the exact
// WorkloadInstance endpoint the RUNS_ON writer MATCHes.
//
// One source repository carries one content fact per repo-to-repo relationship
// type, each naming a DISTINCT target repository so those six edges are
// trivially separable by (type, target) alone:
//
//   - PROVISIONS_DEPENDENCY_FOR: Terraform `app_repo = "..."` literal
//     (relationships.EvidenceKindTerraformAppRepo), the same evidence kind
//     odu:repo-dependency-concurrency already proves at the evidence layer
//     (repo_dependency_odu.go) -- this Odù proves it through the full
//     materialization seam instead.
//   - USES_MODULE: a Terraform `module "x" { source = "../../modules/<target>" }`
//     local relative path (relationships.EvidenceKindTerraformModuleSource),
//     mirroring TestDiscoverTerraformLocalModuleSourceEvidence's fixture
//     shape (evidence_terraform_test.go).
//   - DISCOVERS_CONFIG_IN: a terragrunt.hcl `config_path = "../<target>"`
//     local relative path (relationships.EvidenceKindTerragruntDependencyConfigPath).
//   - DEPENDS_ON: a Docker Compose service's `depends_on: [<target>]`
//     (relationships.EvidenceKindDockerComposeDependsOn).
//   - DEPLOYS_FROM: a Docker Compose service's `build.context: ../<target>`
//     (relationships.EvidenceKindDockerComposeBuildContext).
//   - READS_CONFIG_FROM: a Terraform IAM policy granting `ssm:GetParameter`
//     read access to an `/configd/<target>/*` SSM path
//     (relationships.EvidenceKindTerraformIAMPermission,
//     discoverTerraformIAMSSMConfigReadEvidence).
//
// Two negative-case content facts on the SAME source repository prove the
// catalog matcher does not over-admit:
//
//   - a self-reference (`app_repo` naming the source repository's own alias)
//     -- matcher.match's `entry.RepoID == sourceRepoID` guard must exclude it.
//   - a near-miss alias (`target-provisions-extra`, a superstring of the real
//     `target-provisions` alias but a DIFFERENT single catalog-match token)
//     -- proves the fuzzy tokenizer does not treat a prefix collision as a
//     match.
const (
	repoDependencyFamilyOduName      = "odu:ifa-repo-dependency-family"
	repoDependencyFamilyScopeID      = "scope-ifa-repo-dependency-repo-dependency-family-source"
	repoDependencyFamilyGenerationID = "gen-ifa-repo-dependency-repo-dependency-family-source-1"
	repoDependencyFamilySourceRunID  = "run-ifa-repo-dependency-family-1"

	repoDependencyFamilySourceRepoID = "repo-ifa-repo-dependency-source"
	repoDependencyFamilySourceName   = "repo-dependency-family-source"
	repoDependencyFamilySourceSlug   = "ifa-org/repo-dependency-family-source"

	repoDependencyFamilyTargetProvisionsRepoID = "repo-ifa-repo-dependency-target-provisions"
	repoDependencyFamilyTargetProvisionsName   = "target-provisions"

	repoDependencyFamilyTargetUsesModuleRepoID = "repo-ifa-repo-dependency-target-usesmodule"
	repoDependencyFamilyTargetUsesModuleName   = "target-usesmodule"

	repoDependencyFamilyTargetDiscoversConfigRepoID = "repo-ifa-repo-dependency-target-discoversconfig"
	repoDependencyFamilyTargetDiscoversConfigName   = "target-discoversconfig"

	repoDependencyFamilyTargetDependsOnRepoID = "repo-ifa-repo-dependency-target-dependson"
	repoDependencyFamilyTargetDependsOnName   = "target-dependson"

	repoDependencyFamilyTargetDeploysFromRepoID = "repo-ifa-repo-dependency-target-deploysfrom"
	repoDependencyFamilyTargetDeploysFromName   = "target-deploysfrom"

	repoDependencyFamilyTargetReadsConfigRepoID = "repo-ifa-repo-dependency-target-readsconfig"
	repoDependencyFamilyTargetReadsConfigName   = "target-readsconfig"

	// repoDependencyFamilyNearMissAlias is a superstring of
	// repoDependencyFamilyTargetProvisionsName that the catalog matcher's
	// whole-token comparison must NOT treat as a match: hyphens are catalog
	// token characters (catalogMatchTokens), so "target-provisions-extra"
	// tokenizes to one token distinct from "target-provisions", not a prefix
	// match.
	repoDependencyFamilyNearMissAlias = "target-provisions-extra"

	repoDependencyFamilyCommitSHA = "0123456789abcdef0123456789abcdef01234567"

	repoDependencyFamilyEnvironment     = "prod"
	repoDependencyFamilyDestinationName = "prod-cluster"
	repoDependencyFamilyArgoCDRepoURL   = "https://github.com/ifa-org/repo-dependency-family-source.git"
)

// Exported repository-dependency family identities let the materializededges
// package exercise the compiled catalog through the production resolver
// without duplicating reference-side values that could drift open.
const (
	RepoDependencyFamilyOduName                     = repoDependencyFamilyOduName
	RepoDependencyFamilySourceRepoID                = repoDependencyFamilySourceRepoID
	RepoDependencyFamilySourceName                  = repoDependencyFamilySourceName
	RepoDependencyFamilyTargetUsesModuleRepoID      = repoDependencyFamilyTargetUsesModuleRepoID
	RepoDependencyFamilyTargetDiscoversConfigRepoID = repoDependencyFamilyTargetDiscoversConfigRepoID
	RepoDependencyFamilyTargetDependsOnRepoID       = repoDependencyFamilyTargetDependsOnRepoID
	RepoDependencyFamilyTargetDeploysFromRepoID     = repoDependencyFamilyTargetDeploysFromRepoID
	RepoDependencyFamilyTargetReadsConfigRepoID     = repoDependencyFamilyTargetReadsConfigRepoID
	RepoDependencyFamilyEnvironment                 = repoDependencyFamilyEnvironment
	RepoDependencyFamilyDestinationName             = repoDependencyFamilyDestinationName
)

// repoDependencyFamilyOdu returns the binary-portable catalog representation
// of the repo_dependency family fixture.
func repoDependencyFamilyOdu() CatalogOdu {
	factsForOdu := []facts.Envelope{
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilyTargetProvisionsRepoID, repoDependencyFamilyTargetProvisionsName, "ifa-org/"+repoDependencyFamilyTargetProvisionsName)),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilyTargetUsesModuleRepoID, repoDependencyFamilyTargetUsesModuleName, "ifa-org/"+repoDependencyFamilyTargetUsesModuleName)),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilyTargetDiscoversConfigRepoID, repoDependencyFamilyTargetDiscoversConfigName, "ifa-org/"+repoDependencyFamilyTargetDiscoversConfigName)),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilyTargetDependsOnRepoID, repoDependencyFamilyTargetDependsOnName, "ifa-org/"+repoDependencyFamilyTargetDependsOnName)),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilyTargetDeploysFromRepoID, repoDependencyFamilyTargetDeploysFromName, "ifa-org/"+repoDependencyFamilyTargetDeploysFromName)),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilyTargetReadsConfigRepoID, repoDependencyFamilyTargetReadsConfigName, "ifa-org/"+repoDependencyFamilyTargetReadsConfigName)),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyRepository(repoDependencyFamilySourceRepoID, repoDependencyFamilySourceName, repoDependencyFamilySourceSlug)),
		repoDependencyFamilyFileFact(codegraphv1.File{
			RepoID:       repoDependencyFamilySourceRepoID,
			RelativePath: "deploy/application.yaml",
			Language:     strPtr("yaml"),
			ParsedFileData: map[string]any{
				"k8s_resources": []any{
					map[string]any{"kind": "Deployment", "namespace": repoDependencyFamilyEnvironment},
				},
			},
		}, "argocd", "apiVersion: argoproj.io/v1alpha1\n"+
			"kind: Application\n"+
			"spec:\n"+
			"  destination:\n"+
			"    name: "+repoDependencyFamilyDestinationName+"\n"+
			"    namespace: "+repoDependencyFamilyEnvironment+"\n"+
			"  source:\n"+
			"    repoURL: "+repoDependencyFamilyArgoCDRepoURL+"\n"),

		// Positive repo-to-repo cases: one content fact per relationship type.
		repoDependencyFamilyContentFact(
			"env/ifa-repo-dependency/app-repo.tf", "terraform_hcl",
			`app_repo = "`+repoDependencyFamilyTargetProvisionsName+`"`+"\n",
		),
		repoDependencyFamilyContentFact(
			"env/ifa-repo-dependency/module.tf", "terraform_hcl",
			"module \"queue\" {\n  source = \"../../modules/"+repoDependencyFamilyTargetUsesModuleName+"\"\n}\n",
		),
		repoDependencyFamilyContentFact(
			"env/ifa-repo-dependency/terragrunt.hcl", "terragrunt",
			`config_path = "../`+repoDependencyFamilyTargetDiscoversConfigName+`"`+"\n",
		),
		repoDependencyFamilyContentFact(
			"deploy/docker-compose.yml", "docker_compose",
			"services:\n  app:\n    depends_on:\n      - "+repoDependencyFamilyTargetDependsOnName+"\n",
		),
		repoDependencyFamilyContentFact(
			"deploy/docker-compose-build.yml", "docker_compose",
			"services:\n  app:\n    build:\n      context: ../"+repoDependencyFamilyTargetDeploysFromName+"\n",
		),
		repoDependencyFamilyContentFact(
			"env/ifa-repo-dependency/iam.tf", "terraform_hcl",
			"resource \"aws_iam_policy\" \"read_config\" {\n"+
				"  policy = jsonencode({\n"+
				"    Statement = [{\n"+
				"      Effect   = \"Allow\"\n"+
				"      Action   = [\"ssm:GetParameter\", \"ssm:GetParameters\"]\n"+
				"      Resource = \"arn:aws:ssm:us-east-1:123456789012:parameter/configd/"+repoDependencyFamilyTargetReadsConfigName+"/*\"\n"+
				"    }]\n"+
				"  })\n"+
				"}\n",
		),

		// Negative cases: must produce zero evidence.
		repoDependencyFamilyContentFact(
			"env/ifa-repo-dependency/self.tf", "terraform_hcl",
			`app_repo = "`+repoDependencyFamilySourceName+`"`+"\n",
		),
		repoDependencyFamilyContentFact(
			"env/ifa-repo-dependency/prefix.tf", "terraform_hcl",
			`app_repo = "`+repoDependencyFamilyNearMissAlias+`"`+"\n",
		),
		repoDependencyFamilyFollowupFact("workload_materialization", "workload:"+repoDependencyFamilySourceName),
		repoDependencyFamilyFollowupFact("deployment_mapping", "deployment:"+repoDependencyFamilySourceName),
	}
	return CatalogOdu{
		Odu: Odu{Name: repoDependencyFamilyOduName, Facts: factsForOdu},
		Detail: "seven repository scopes and 18 facts, with six target-only scopes first and one evidence-bearing source scope last; the source has six content facts each producing exactly one repo-to-repo dependency type against a distinct target repository " +
			"(PROVISIONS_DEPENDENCY_FOR, USES_MODULE, DISCOVERS_CONFIG_IN, DEPENDS_ON, DEPLOYS_FROM, READS_CONFIG_FROM), " +
			"plus one ArgoCD/Kubernetes file fact producing a RUNS_ON relationship from the source repository's prod WorkloadInstance to the prod-cluster Platform, " +
			"plus a self-reference and a near-miss-alias content fact that must produce zero evidence -- " +
			"proving the production discovery, resolution, workload projection, and repo_dependency extraction seams reproduce exactly the seven-edge set.",
	}
}

// RepoDependencyFamilyOdu returns the compiled family fixture used by the
// catalog and by the materializededges package's moved vacuity-guard tests.
func RepoDependencyFamilyOdu() CatalogOdu {
	return repoDependencyFamilyOdu()
}

func repoDependencyFamilyRepository(repoID, name, repoSlug string) codegraphv1.Repository {
	graphKind := "repository"
	parsedFileCount := "0"
	isDependency := repoID != repoDependencyFamilySourceRepoID
	remoteURL := "https://github.com/ifa-org/" + name + ".git"
	localPath := "/fixtures/ifa/" + name
	sourceRunID := repoDependencyFamilySourceRunID
	return codegraphv1.Repository{
		RepoID: repoID, GraphID: &repoID, GraphKind: &graphKind, Name: &name,
		ParsedFileCount: &parsedFileCount, IsDependency: &isDependency,
		RepoSlug: &repoSlug, RemoteURL: &remoteURL, LocalPath: &localPath,
		SourceRunID: &sourceRunID,
	}
}

// repoDependencyFamilyRepositoryFact encodes one typed repository contract
// before assigning the family's per-repository scope and generation.
func repoDependencyFamilyRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	if repository.Name == nil || *repository.Name == "" {
		panic(fmt.Sprintf("ifa: repo-dependency catalog repository %q has no name", repository.RepoID))
	}
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode repo-dependency catalog repository %q: %v", repository.RepoID, err))
	}
	scopeID, generationID := repoDependencyFamilyRepoCoordinates(*repository.Name)
	return repoDependencyFamilyFactAt(scopeID, generationID, factschema.FactKindCodegraphRepository, "repository:"+repository.RepoID, payload)
}

func repoDependencyFamilyRepoCoordinates(name string) (string, string) {
	return "scope-ifa-repo-dependency-" + name, "gen-ifa-repo-dependency-" + name + "-1"
}

// repoDependencyFamilyFileFact builds a typed file fact and adds the raw
// content fields the relationship-evidence discovery seam consumes. The file
// contract intentionally leaves raw content open while requiring repo_id,
// relative_path, and parsed_file_data, so one fact can drive both production
// workload projection and ArgoCD destination discovery.
func repoDependencyFamilyFileFact(file codegraphv1.File, artifactType, content string) facts.Envelope {
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode repo-dependency catalog file %q: %v", file.RelativePath, err))
	}
	payload["artifact_type"] = artifactType
	payload["commit_sha"] = repoDependencyFamilyCommitSHA
	payload["content"] = content
	return repoDependencyFamilyFact(factschema.FactKindCodegraphFile, "file:"+file.RepoID+":"+file.RelativePath, payload)
}

// repoDependencyFamilyContentFact builds one raw "content" fact on the
// family's single source repository, in the content_path/content_body shape
// the git-content collector emits (relationships.envelopeContentIdentity's
// documented fallback pair) and odu:repo-dependency-concurrency's
// repoDependencyContentFact already proves satisfies fact-kind validation.
func repoDependencyFamilyContentFact(path, artifactType, body string) facts.Envelope {
	return repoDependencyFamilyFact(contentFactKind, "content:"+repoDependencyFamilySourceRepoID+":"+path, map[string]any{
		"artifact_type": artifactType,
		"commit_sha":    repoDependencyFamilyCommitSHA,
		"content_body":  body,
		"content_path":  path,
		"repo_id":       repoDependencyFamilySourceRepoID,
	})
}

func repoDependencyFamilyFollowupFact(domain, entityKey string) facts.Envelope {
	return repoDependencyFamilyFact("shared_followup", "shared_followup:"+repoDependencyFamilySourceRepoID+":"+domain, map[string]any{
		"reducer_domain": domain,
		"entity_key":     entityKey,
		"reason":         "repository snapshot emitted " + domain + " follow-up",
		"repo_id":        repoDependencyFamilySourceRepoID,
	})
}

// repoDependencyFamilyFact stamps the family's evidence-bearing source
// coordinates onto one fact envelope. Deliberately mirrors
// deployableUnitCatalogFact's field
// set exactly (no FactID, ObservedAt, or SourceRef): both the compiled Odù
// and its cassette-loaded projection must render identical zero-value
// envelopes on those fields for
// TestRepoDependencyFamilyCassetteMatchesCompiledCatalog's reflect.DeepEqual
// to hold.
func repoDependencyFamilyFact(kind, stableKey string, payload map[string]any) facts.Envelope {
	return repoDependencyFamilyFactAt(repoDependencyFamilyScopeID, repoDependencyFamilyGenerationID, kind, stableKey, payload)
}

func repoDependencyFamilyFactAt(scopeID, generationID, kind, stableKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID: scopeID, GenerationID: generationID, FactKind: kind,
		StableFactKey: stableKey, SchemaVersion: "1.0.0", CollectorKind: "git",
		SourceConfidence: "observed", Payload: payload,
	}
}
