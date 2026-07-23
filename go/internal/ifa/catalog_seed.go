// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// repositoryFactKind is the raw fact-kind literal for a repository fact. It
// has no exported constant in go/internal/facts (the kind is admission-exempt,
// #4752) so the string is spelled out, matching the registry entry's Kind
// value and go/internal/storage/postgres's own repository-kind checks.
const repositoryFactKind = "repository"

// contentFactKind is the raw fact-kind literal the git-content collector
// emits (go/internal/collector/git_content_fact_envelopes.go). It has no
// registry entry (#4783 W1): relationships.DiscoverEvidence dispatches
// artifact-type/content evidence off this unregistered kind, not off a typed
// registered one, so Ifá seeds it as a plain string literal too.
const contentFactKind = "content"

// contentEntityFactKind and fileFactKind are the raw fact-kind literals the
// git collector emits for a parsed entity and a parsed file
// (go/internal/collector/git_content_fact_envelopes.go, git_fact_builder.go).
// Neither carries a registry payload schema (content_entity is absent from
// specs/fact-kind-registry.v1.yaml entirely, matching
// go/internal/reducer/fact_kind_loader.go's factKindContentEntity/
// factKindFile), so Ifá seeds them as plain string literals, mirroring
// repositoryFactKind/contentFactKind above.
const (
	contentEntityFactKind = "content_entity"
	fileFactKind          = "file"
)

// catalogSeed is the P1 seed set of cataloged Odùs. Every entry here is
// genuinely green: it either satisfies a payload schema (fact_kind:*) or
// produces graph evidence relationships.DiscoverEvidence resolves against a
// same-Odù repository fact (narrowed_correlation:*). Adding a fixture here
// without a matching, honestly-green specs/ifa-coverage-manifest.v1.yaml row
// would be a false-green coverage claim (see coverage_falsegreen_test.go).
var catalogSeed = []CatalogOdu{
	kustomizeDeploysFromOdu(),
	argocdDeploysFromOdu(),
	awsPackOdu(),
	vulnPackOdu(),
	demoOrgRoundtripOdu(),
	repoDependencyConcurrencyOdu(),
	sqlFamilyOdu(),
	sqlFamilyDeltaOdu(),
}

// awsFamilySchemaBackedKinds are the representative aws_* fact kinds
// odu:aws-pack carries with fixturepack.ValidPayload examples, proving the
// payload-schema derivation axis (design §1c) for the AWS cloud-inventory
// family. awsTagObservationKind additionally proves the schema-less
// registry-only path (facts.AWSTagObservationFactKind carries no
// PayloadSchema) in the same Odù.
var awsFamilySchemaBackedKinds = []string{
	facts.AWSResourceFactKind,
	facts.AWSResourcePolicyPermissionFactKind,
	facts.AWSSecurityGroupRuleFactKind,
	facts.AWSWarningFactKind,
}

// awsPackOdu carries one valid fixturepack payload per representative aws_*
// fact kind, plus one registry-only (schema-less) aws_tag_observation fact.
// It has no repository fact and produces no graph evidence — it exists purely
// to prove fact_kind:* payload-schema coverage (fact_kind:aws_resource in
// specs/ifa-coverage-manifest.v1.yaml), not narrowed_correlation coverage.
func awsPackOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(awsFamilySchemaBackedKinds)+1)
	for _, kind := range awsFamilySchemaBackedKinds {
		payload, ok := fixturepack.ValidPayload(kind)
		if !ok {
			panic(fmt.Sprintf("ifa: catalog_seed odu:aws-pack: fixturepack has no valid payload example for %q", kind))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:  "aws:sandbox-account",
			FactKind: kind,
			Payload:  payload,
		})
	}
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:  "aws:sandbox-account",
		FactKind: facts.AWSTagObservationFactKind,
		Payload: map[string]any{
			"resource_id": "vpc-0abc123def456",
			"key":         "env",
			"value":       "prod",
		},
	})

	return CatalogOdu{
		Odu:    Odu{Name: "odu:aws-pack", Facts: factsForOdu},
		Detail: "fixturepack-valid payloads for the aws_resource/aws_resource_policy_permission/aws_security_group_rule/aws_warning family, plus the schema-less aws_tag_observation registry-only kind",
	}
}

// vulnFamilySchemaBackedKinds are the schema-backed vulnerability_intelligence
// fact kinds odu:vuln-pack carries with fixturepack.ValidPayload examples,
// proving the payload-schema derivation axis (design §1c) for the
// vulnerability-intelligence family (epic #5462). These are the 10 kinds the
// registry's payload_schema_overrides names for the family
// (specs/fact-kind-registry.v1.yaml); the 11th kind,
// facts.VulnerabilityWarningFactKind, is registry-only (no payload schema) and
// is carried separately below to prove the schema-less presence path in the
// same Odù, mirroring awsPackOdu's aws_tag_observation.
var vulnFamilySchemaBackedKinds = []string{
	facts.VulnerabilityOSPackageFactKind,
	facts.VulnerabilityCVEFactKind,
	facts.VulnerabilityAffectedPackageFactKind,
	facts.VulnerabilityAffectedProductFactKind,
	facts.VulnerabilityEPSSScoreFactKind,
	facts.VulnerabilityKnownExploitedFactKind,
	facts.VulnerabilityGoModuleEvidenceFactKind,
	facts.VulnerabilityGoCallReachabilityFactKind,
	facts.VulnerabilityReferenceFactKind,
	facts.VulnerabilitySourceSnapshotFactKind,
}

// vulnPackOdu carries one valid fixturepack payload per schema-backed
// vulnerability_intelligence fact kind, plus one registry-only (schema-less)
// vulnerability.warning fact. Like awsPackOdu it has no repository fact and
// produces no graph evidence — it exists purely to prove fact_kind:* payload-
// schema coverage for the 11 vulnerability_intelligence kinds
// (specs/ifa-coverage-manifest.v1.yaml), the Ifá backfill this family owns per
// epic #5462 and the #5474 coverage-backfill plan's ownership carve-out. It
// does not prove narrowed_correlation coverage.
func vulnPackOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(vulnFamilySchemaBackedKinds)+1)
	for _, kind := range vulnFamilySchemaBackedKinds {
		payload, ok := fixturepack.ValidPayload(kind)
		if !ok {
			panic(fmt.Sprintf("ifa: catalog_seed odu:vuln-pack: fixturepack has no valid payload example for %q", kind))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:  "vulnerability_intelligence:scanned-image",
			FactKind: kind,
			Payload:  payload,
		})
	}
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:  "vulnerability_intelligence:scanned-image",
		FactKind: facts.VulnerabilityWarningFactKind,
		Payload: map[string]any{
			"code":    "advisory_source_unavailable",
			"message": "advisory source temporarily unavailable during collection",
			"source":  "debian",
		},
	})

	return CatalogOdu{
		Odu:    Odu{Name: "odu:vuln-pack", Facts: factsForOdu},
		Detail: "fixturepack-valid payloads for the schema-backed vulnerability_intelligence family (os_package/cve/affected_package/affected_product/epss_score/known_exploited/go_module_evidence/go_call_reachability/reference/source_snapshot), plus the schema-less vulnerability.warning registry-only kind",
	}
}

// kustomizeDeploysFromOdu carries a Kustomize overlay content fact in a
// "repo-deploy" source scope referencing the "repo-payments" target
// repository's "payments-service" alias, mirroring
// relationships.TestDiscoverKustomizeEvidence. relationships.DiscoverEvidence
// resolves it to a DEPLOYS_FROM edge carrying KUSTOMIZE_RESOURCE_REFERENCE
// evidence — the exact evidence_kinds filter B-12's rc-29 requires.
func kustomizeDeploysFromOdu() CatalogOdu {
	odu := Odu{
		Name: "odu:kustomize-deploys-from",
		Facts: []facts.Envelope{
			targetRepositoryFact(),
			{
				ScopeID:  "repo-deploy",
				FactKind: contentFactKind,
				Payload: map[string]any{
					"relative_path": "overlays/prod/kustomization.yaml",
					"content":       "resources:\n  - ../../base\nnamePrefix: payments-service\n",
				},
			},
		},
	}
	return CatalogOdu{
		Odu:    odu,
		Detail: "Kustomize overlay DEPLOYS_FROM evidence resolving to the cataloged payments-service repository (rc-29's KUSTOMIZE_RESOURCE_REFERENCE filter)",
	}
}

// argocdDeploysFromOdu carries an ArgoCD Application content fact referencing
// the same "repo-payments" target, mirroring
// relationships.TestDiscoverArgoCDEvidence. It produces the same DEPLOYS_FROM
// relationship as the Kustomize Odù but with ARGOCD_APPLICATION_SOURCE
// evidence, not KUSTOMIZE_RESOURCE_REFERENCE — the deliberate false-green
// break in coverage_falsegreen_test.go.
func argocdDeploysFromOdu() CatalogOdu {
	odu := Odu{
		Name: "odu:argocd-deploys-from",
		Facts: []facts.Envelope{
			targetRepositoryFact(),
			{
				ScopeID:  "repo-gitops",
				FactKind: contentFactKind,
				Payload: map[string]any{
					"artifact_type": "argocd",
					"relative_path": "apps/payments.yaml",
					"content": "apiVersion: argoproj.io/v1alpha1\n" +
						"kind: Application\n" +
						"spec:\n" +
						"  source:\n" +
						"    repoURL: 'https://github.com/myorg/payments-service.git'\n" +
						"    targetRevision: HEAD\n",
				},
			},
		},
	}
	return CatalogOdu{
		Odu:    odu,
		Detail: "ArgoCD Application DEPLOYS_FROM evidence resolving to the same cataloged repository, carrying ARGOCD_APPLICATION_SOURCE (not KUSTOMIZE_RESOURCE_REFERENCE) evidence",
	}
}

// targetRepositoryFact is the shared "repo-payments" repository fact both
// deploy-source Odùs anchor their catalog on. Its payload satisfies
// fixturepack's repository.v1.schema.json (repo_id is the only required
// field) so ValidateOduPayloads passes it as well as RepositoryCatalog
// deriving the "payments-service" alias.
func targetRepositoryFact() facts.Envelope {
	return facts.Envelope{
		ScopeID:  "repo-payments",
		FactKind: repositoryFactKind,
		Payload: map[string]any{
			"repo_id":   "repo-payments",
			"name":      "payments-service",
			"repo_slug": "acme/payments-service",
		},
	}
}
