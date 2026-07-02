// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/correlation/rules"

func deployableUnitRulePack(candidate WorkloadCandidate) rules.RulePack {
	switch {
	case hasProvenance(candidate.Provenance, "argocd_application_source", "argocd_applicationset_deploy_source"):
		return rules.ArgoCDRulePack()
	case hasProvenance(candidate.Provenance, "kustomize_resource"):
		return rules.KustomizeRulePack()
	case hasProvenance(candidate.Provenance, "helm_deployment"):
		return rules.HelmRulePack()
	case hasProvenance(candidate.Provenance, "jenkins_pipeline") &&
		hasProvenance(candidate.Provenance, "dockerfile_runtime"):
		return rules.JenkinsRulePack()
	case hasProvenance(candidate.Provenance, "github_actions_workflow") &&
		hasProvenance(candidate.Provenance, "dockerfile_runtime"):
		return rules.GitHubActionsRulePack()
	case hasProvenance(candidate.Provenance, "dockerfile_runtime"):
		return rules.DockerfileRulePack()
	case hasProvenance(candidate.Provenance, "docker_compose_runtime"):
		return rules.DockerComposeRulePack()
	case hasProvenance(candidate.Provenance, "cloudformation_template"):
		return rules.CloudFormationRulePack()
	case hasProvenance(candidate.Provenance, "jenkins_pipeline"):
		return rules.JenkinsRulePack()
	case hasProvenance(candidate.Provenance, "github_actions_workflow"):
		return rules.GitHubActionsRulePack()
	default:
		return rules.RulePack{
			Name:                   "deployable-unit-fallback",
			MinAdmissionConfidence: deployableUnitCorrelationFallbackThreshold,
			Rules: []rules.Rule{
				{Name: "extract-normalized-deployable-unit-key", Kind: rules.RuleKindExtractKey, Priority: 10},
				{Name: "match-evidence-within-bounded-scope", Kind: rules.RuleKindMatch, Priority: 20, MaxMatches: 8},
				{Name: "derive-admission-shape", Kind: rules.RuleKindDerive, Priority: 30},
				{Name: "admit-strong-runtime-evidence", Kind: rules.RuleKindAdmit, Priority: 40},
				{Name: "explain-correlation-decision", Kind: rules.RuleKindExplain, Priority: 50},
			},
		}
	}
}
