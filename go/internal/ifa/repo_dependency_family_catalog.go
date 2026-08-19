// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The repo_dependency family Odù (#5999, under the #5543 umbrella).
//
// Unlike deployable_unit_edges, this family's SIX repo-to-repo relationship
// types (DEPENDS_ON, DEPLOYS_FROM, DISCOVERS_CONFIG_IN,
// PROVISIONS_DEPENDENCY_FOR, USES_MODULE, READS_CONFIG_FROM -- the
// alternation cypher.RepoDependencyMaterializedEdgeTypes splits out of
// repoDependencyRelationshipEdgeTypes) derive from facts ALONE: each is a
// relationships.EvidenceFact the production discovery layer
// (relationships.DiscoverEvidence) emits directly off one raw "content"
// fact's artifact_type/body, with no resolved-relationship precondition the
// way deployable_unit_edges' deployment_repo_id has. So this Odù needs no
// second production seam beyond DiscoveredEvidence -> relationships.Resolve
// (see materialized_edges_repo_dependency.go).
//
// One source repository carries one content fact per relationship type, each
// naming a DISTINCT target repository so the six edges are trivially
// separable by (type, target) alone:
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
//
// RUNS_ON, the seventh registry type, is a deliberate, explicit, documented
// gap -- NOT proven by this Odù. See
// materialized_edges_repo_dependency.go's package-level doc comment for why:
// its Platform-id endpoint identity is not reachable from an Odù's own
// facts, distinct from the PERMANENT exclusion of
// HAS_DEPLOYMENT_EVIDENCE/EVIDENCES_REPOSITORY_RELATIONSHIP/
// TARGETS_ENVIRONMENT that cypher.RepoDependencyMaterializedEdgeTypes never
// even registers.
const (
	repoDependencyFamilyOduName      = "odu:ifa-repo-dependency-family"
	repoDependencyFamilyScopeID      = "scope-ifa-repo-dependency-family"
	repoDependencyFamilyGenerationID = "gen-ifa-repo-dependency-family-1"
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
)

// repoDependencyFamilyOdu returns the binary-portable catalog representation
// of the repo_dependency family fixture.
func repoDependencyFamilyOdu() CatalogOdu {
	factsForOdu := []facts.Envelope{
		repoDependencyFamilyRepositoryFact(repoDependencyFamilySourceRepoID, repoDependencyFamilySourceName, repoDependencyFamilySourceSlug),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyTargetProvisionsRepoID, repoDependencyFamilyTargetProvisionsName, "ifa-org/"+repoDependencyFamilyTargetProvisionsName),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyTargetUsesModuleRepoID, repoDependencyFamilyTargetUsesModuleName, "ifa-org/"+repoDependencyFamilyTargetUsesModuleName),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyTargetDiscoversConfigRepoID, repoDependencyFamilyTargetDiscoversConfigName, "ifa-org/"+repoDependencyFamilyTargetDiscoversConfigName),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyTargetDependsOnRepoID, repoDependencyFamilyTargetDependsOnName, "ifa-org/"+repoDependencyFamilyTargetDependsOnName),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyTargetDeploysFromRepoID, repoDependencyFamilyTargetDeploysFromName, "ifa-org/"+repoDependencyFamilyTargetDeploysFromName),
		repoDependencyFamilyRepositoryFact(repoDependencyFamilyTargetReadsConfigRepoID, repoDependencyFamilyTargetReadsConfigName, "ifa-org/"+repoDependencyFamilyTargetReadsConfigName),

		// Positive cases: one content fact per registry type this Odù proves.
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
	}
	return CatalogOdu{
		Odu: Odu{Name: repoDependencyFamilyOduName, Facts: factsForOdu},
		Detail: "one source repository with six content facts each producing exactly one repo_dependency relationship type against a distinct target repository " +
			"(PROVISIONS_DEPENDENCY_FOR, USES_MODULE, DISCOVERS_CONFIG_IN, DEPENDS_ON, DEPLOYS_FROM, READS_CONFIG_FROM), " +
			"plus a self-reference and a near-miss-alias content fact that must produce zero evidence -- " +
			"proving DiscoveredEvidence -> relationships.Resolve -> reducer.ExtractRepoDependencyIntentRows reproduces exactly the six-edge set. " +
			"RUNS_ON (the registry's seventh type) is a deliberate, documented non-goal of this Odù.",
	}
}

// repoDependencyFamilyRepositoryFact builds one raw "repository" fact in the
// shape relationships.RepositoryCatalogEntry reads (repo_id, name,
// repo_slug), the same untyped shape odu:repo-dependency-concurrency already
// proves satisfies the fact-kind registry (repo_dependency_odu.go's
// repoDependencyRepositoryFact) -- deliberately NOT the typed
// codegraphv1.Repository/factschema.EncodeCodegraphRepository shape
// deployable_unit_family_catalog.go uses, because this family's evidence
// discovery needs only the untyped alias fields, not a full codegraph
// Repository contract.
func repoDependencyFamilyRepositoryFact(repoID, name, repoSlug string) facts.Envelope {
	return repoDependencyFamilyFact(repositoryFactKind, "repository:"+repoID, map[string]any{
		"name":          name,
		"repo_id":       repoID,
		"repo_slug":     repoSlug,
		"source_run_id": repoDependencyFamilySourceRunID,
	})
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

// repoDependencyFamilyFact stamps the family's single scope/generation onto
// one fact envelope. Deliberately mirrors deployableUnitCatalogFact's field
// set exactly (no FactID, ObservedAt, or SourceRef): both the compiled Odù
// and its cassette-loaded projection must render identical zero-value
// envelopes on those fields for
// TestRepoDependencyFamilyCassetteMatchesCompiledCatalog's reflect.DeepEqual
// to hold.
func repoDependencyFamilyFact(kind, stableKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID: repoDependencyFamilyScopeID, GenerationID: repoDependencyFamilyGenerationID, FactKind: kind,
		StableFactKey: stableKey, SchemaVersion: "1.0.0", CollectorKind: "git",
		SourceConfidence: "observed", Payload: payload,
	}
}
