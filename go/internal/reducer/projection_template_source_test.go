// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
)

// TestBuildProjectionRowsInfersKubernetesPlatformFromTemplateSourceProvenance
// guards the #7767 platform half: a Dockerfile-only service repository whose
// only deployment evidence is an ApplicationSet template source bound to a
// deployment repository must still get a kubernetes runtime platform.
func TestBuildProjectionRowsInfersKubernetesPlatformFromTemplateSourceProvenance(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{{
		RepoID:           "repo-service",
		RepoName:         "service-api",
		DeploymentRepoID: "repo-gitops",
		Classification:   "service",
		Confidence:       0.95,
		Provenance:       []string{"dockerfile_runtime", "argocd_applicationset_template_source"},
	}}
	deploymentEnvs := map[string][]string{"repo-gitops": {"prod"}}

	result := BuildProjectionRows(candidates, deploymentEnvs)

	if got := len(result.RuntimePlatformRows); got != 1 {
		t.Fatalf("len(RuntimePlatformRows) = %d, want 1", got)
	}
	if got := result.RuntimePlatformRows[0].PlatformKind; got != "kubernetes" {
		t.Fatalf("PlatformKind = %q, want kubernetes", got)
	}
}

// TestTemplateSourceProvenanceIsAServiceClassificationSignal keeps
// hasServiceClassificationSignals in step with the other Argo provenances.
func TestTemplateSourceProvenanceIsAServiceClassificationSignal(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{Provenance: []string{"argocd_applicationset_template_source"}}
	if !hasServiceClassificationSignals(candidate) {
		t.Fatal("hasServiceClassificationSignals = false, want true")
	}
}

// TestDeployableUnitRulePackUsesArgoCDForTemplateSourceProvenance keeps the
// template-source provenance on the same rule pack as the other Argo
// provenances.
func TestDeployableUnitRulePackUsesArgoCDForTemplateSourceProvenance(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{Provenance: []string{"dockerfile_runtime", "argocd_applicationset_template_source"}}
	if got := deployableUnitRulePack(candidate).Name; got != "argocd" {
		t.Fatalf("deployableUnitRulePack = %q, want argocd", got)
	}
}

// TestArgoCDRulePackAdmitsTemplateSourceOnlyCandidate proves a candidate whose
// only deployment evidence is the ApplicationSet template-source provenance, at
// the 0.95 the evidence kind carries, clears the ArgoCD rule pack's 0.9
// admission threshold, exactly as the sibling deploy-source provenance does.
func TestArgoCDRulePackAdmitsTemplateSourceOnlyCandidate(t *testing.T) {
	t.Parallel()

	for _, provenance := range []string{
		"argocd_applicationset_deploy_source",
		"argocd_applicationset_template_source",
	} {
		t.Run(provenance, func(t *testing.T) {
			t.Parallel()
			candidate := WorkloadCandidate{
				RepoID:           "repo-service",
				RepoName:         "service-api",
				DeploymentRepoID: "repo-gitops",
				Classification:   "service",
				Confidence:       0.95,
				Provenance:       []string{provenance},
			}

			evaluation, err := evaluateDeployableUnitCandidates(deployableUnitIntent("service-api"), []WorkloadCandidate{candidate})
			if err != nil {
				t.Fatalf("evaluateDeployableUnitCandidates() error = %v", err)
			}
			if len(evaluation.Results) == 0 {
				t.Fatal("evaluation has no results")
			}
			for _, result := range evaluation.Results {
				if result.Candidate.State != correlationmodel.CandidateStateAdmitted {
					t.Fatalf("candidate state = %q, want admitted (%s): %#v",
						result.Candidate.State, provenance, result.Candidate)
				}
			}
		})
	}
}
