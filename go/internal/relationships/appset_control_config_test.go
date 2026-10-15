// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// wantControlConfigFacts asserts the facts an ApplicationSet whose git
// generator reads config from the control repository itself (issue #7778: G
// is C, template source is a third repository D) must produce: no discovery
// fact (control -> control would be a self-loop), one control-to-deployed
// template-source DEPLOYS_FROM at 0.95 whose Details name the control
// repository as the config repository and carry no self_reference flag, and
// wantPlatform destination-platform facts. stats is the
// DiscoverEvidenceWithStats tally for the same input.
func wantControlConfigFacts(t *testing.T, evidence []EvidenceFact, stats DiscoveryStats, wantPlatform int, wantStats ApplicationSetTemplateSourceStats) {
	t.Helper()

	if discovery := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery); len(discovery) != 0 {
		t.Fatalf("discovery evidence = %#v, want none (control -> control would be a self-loop)", discovery)
	}
	if deploy := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource); len(deploy) != 0 {
		t.Fatalf("deploy-source evidence = %#v, want none (the edge runs control -> deployed)", deploy)
	}
	template := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource)
	if len(template) != 1 {
		t.Fatalf("template-source evidence = %d, want 1: %#v", len(template), template)
	}
	fact := template[0]
	if fact.RelationshipType != RelDeploysFrom || fact.SourceRepoID != "repo-gitops" || fact.TargetRepoID != "repo-other" {
		t.Fatalf("template source = %s %s -> %s, want DEPLOYS_FROM repo-gitops -> repo-other",
			fact.RelationshipType, fact.SourceRepoID, fact.TargetRepoID)
	}
	want := DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDApplicationSetTemplateSource)
	if fact.Confidence != want || fact.Confidence != 0.95 {
		t.Fatalf("template source confidence = %v, registry = %v, want 0.95", fact.Confidence, want)
	}
	if got := fact.Details["control_plane_repo_id"]; got != "repo-gitops" {
		t.Fatalf("control_plane_repo_id = %#v, want repo-gitops", got)
	}
	if got := fact.Details["config_repo_id"]; got != "repo-gitops" {
		t.Fatalf("config_repo_id = %#v, want repo-gitops", got)
	}
	if _, ok := fact.Details["self_reference"]; ok {
		t.Fatalf("self_reference = %#v, want absent (the template source is not the generator's config repository)", fact.Details["self_reference"])
	}
	for key, want := range map[string]any{
		"deploy_repo_url": "https://github.com/myorg/other-service",
		"discovery_path":  "argocd/svc/overlays/*/config.yaml",
		"matched_alias":   "other-service",
		"extractor":       "argocd",
		"path":            "applicationsets/svc.yaml",
	} {
		if got := fact.Details[key]; got != want {
			t.Fatalf("Details[%q] = %#v, want %#v", key, got, want)
		}
	}

	platform := evidenceOfKind(evidence, EvidenceKindArgoCDDestinationPlatform)
	if len(platform) != wantPlatform {
		t.Fatalf("platform evidence = %d, want %d: %#v", len(platform), wantPlatform, platform)
	}
	for _, p := range platform {
		if p.RelationshipType != RelRunsOn || p.SourceRepoID != "repo-other" ||
			p.TargetEntityID != "platform:kubernetes:none:server/kubernetes.default.svc:none:none" {
			t.Fatalf("platform fact = %#v, want RUNS_ON repo-other -> the default kubernetes platform", p)
		}
	}
	for _, f := range evidence {
		if f.SourceRepoID != "" && f.SourceRepoID == f.TargetRepoID {
			t.Fatalf("self-loop evidence %s %s -> %s", f.EvidenceKind, f.SourceRepoID, f.TargetRepoID)
		}
	}
	if stats.ApplicationSetTemplateSource != wantStats {
		t.Fatalf("stats = %+v, want %+v", stats.ApplicationSetTemplateSource, wantStats)
	}
}

// TestApplicationSetControlConfigEmitsControlToDeployedDeploysFrom is the
// #7778 fix: the generator reads config from the control repository itself,
// so the deployed repository is recorded as a DEPLOYS_FROM from the control
// repository and its platform is kept, with no discovery self-loop.
func TestApplicationSetControlConfigEmitsControlToDeployedDeploysFrom(t *testing.T) {
	t.Parallel()
	evidence, stats := DiscoverEvidenceWithStats([]facts.Envelope{
		appSetConfigRepoSet("gitops-repo", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-gitops", "prod", "other-service"),
	}, appSetConfigRepoCatalog)

	wantControlConfigFacts(t, evidence, stats, 1, ApplicationSetTemplateSourceStats{ControlConfig: 1})
}

// TestApplicationSetControlConfigTemplatedDestinationStillDeploysFrom guards
// that the DEPLOYS_FROM edge does not depend on the destination.
func TestApplicationSetControlConfigTemplatedDestinationStillDeploysFrom(t *testing.T) {
	t.Parallel()
	evidence, stats := DiscoverEvidenceWithStats([]facts.Envelope{
		appSetConfigRepoSet("gitops-repo", appSetTemplatedDestination),
		appSetConfigRepoConfig("repo-gitops", "prod", "other-service"),
	}, appSetConfigRepoCatalog)

	wantControlConfigFacts(t, evidence, stats, 0, ApplicationSetTemplateSourceStats{ControlConfig: 1, SkippedTemplatedDestination: 1})
}

// TestApplicationSetControlConfigStructuredPathMatchesYAMLPath runs the
// control-config shape through the parsed_file_data path and expects the same
// facts.
func TestApplicationSetControlConfigStructuredPathMatchesYAMLPath(t *testing.T) {
	t.Parallel()
	evidence, stats := DiscoverEvidenceWithStats([]facts.Envelope{
		appSetStructuredEnvelope("gitops-repo", "other-service", "https://kubernetes.default.svc"),
	}, appSetConfigRepoCatalog)

	wantControlConfigFacts(t, evidence, stats, 1, ApplicationSetTemplateSourceStats{ControlConfig: 1})

	// The structured path also stamps the first-party ref details that the
	// YAML path does not carry.
	fact := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource)[0]
	for key, want := range map[string]any{
		"first_party_ref_kind":       "argocd_applicationset_template_source",
		"first_party_ref_path":       "deploy",
		"argocd_applicationset_name": "svc",
	} {
		if got := fact.Details[key]; got != want {
			t.Fatalf("structured Details[%q] = %#v, want %#v", key, got, want)
		}
	}
}
