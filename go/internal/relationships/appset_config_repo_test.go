// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// appSetConfigRepoCatalog holds the repositories the fixtures below use:
// repo-app is a service repository that also holds its own per-environment
// config, repo-other is a second service repository, repo-central is a shared
// config repository, and repo-gitops holds the ApplicationSet (the control
// repository).
var appSetConfigRepoCatalog = []CatalogEntry{
	{RepoID: "repo-app", Aliases: []string{"app-repo"}},
	{RepoID: "repo-other", Aliases: []string{"other-service"}},
	{RepoID: "repo-central", Aliases: []string{"central-config"}},
	{RepoID: "repo-gitops", Aliases: []string{"gitops-repo"}},
}

// appSetConfigRepoSet builds an ApplicationSet in repo-gitops whose git file
// generator reads config from generatorRepo. The Helm source is an OCI chart
// (the registry host matches no catalog repository) and the values reference is
// {{ .git.repoURL }}, read from each config.yaml. destination is the literal
// destination block ("" for none).
func appSetConfigRepoSet(generatorRepo, destination string) facts.Envelope {
	return facts.Envelope{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"artifact_type": "argocd",
			"relative_path": "applicationsets/svc.yaml",
			"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/` + generatorRepo + `
        files:
          - path: "argocd/svc/overlays/*/config.yaml"
  template:
    spec:
` + destination + `      sources:
        - repoURL: "{{ .helm.repoURL }}"
          chart: "{{ .helm.chart }}"
        - repoURL: "{{ .git.repoURL }}"
          ref: values
`,
		},
	}
}

// appSetConfigRepoConfig is a per-environment config.yaml in scope whose values
// reference names gitRepo.
func appSetConfigRepoConfig(scope, overlay, gitRepo string) facts.Envelope {
	return facts.Envelope{
		ScopeID: scope,
		Payload: map[string]any{
			"artifact_type": "yaml",
			"relative_path": "argocd/svc/overlays/" + overlay + "/config.yaml",
			"content": `helm:
  repoURL: registry.example.invalid
  chart: charts/svc
git:
  repoURL: https://github.com/myorg/` + gitRepo + `
`,
		},
	}
}

const appSetLiteralDestination = `      destination:
        server: https://kubernetes.default.svc
        namespace: svc
`

const appSetTemplatedDestination = `      destination:
        server: "{{ .server }}"
        namespace: "{{ .namespace }}"
`

// evidenceOfKind returns every fact of kind.
func evidenceOfKind(evidence []EvidenceFact, kind EvidenceKind) []EvidenceFact {
	var out []EvidenceFact
	for _, fact := range evidence {
		if fact.EvidenceKind == kind {
			out = append(out, fact)
		}
	}
	return out
}

// TestApplicationSetOtherServiceRepoEmitsDeploySource guards the working shape:
// the config file names a service repository that is not the config repository
// and the OCI chart does not block the edge.
func TestApplicationSetOtherServiceRepoEmitsDeploySource(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("central-config", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-central", "prod", "other-service"),
	}, appSetConfigRepoCatalog)

	deploy := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource)
	if len(deploy) != 1 {
		t.Fatalf("deploy-source evidence = %d, want 1: %#v", len(deploy), deploy)
	}
	if deploy[0].SourceRepoID != "repo-other" || deploy[0].TargetRepoID != "repo-central" {
		t.Fatalf("deploy source %s -> %s, want repo-other -> repo-central", deploy[0].SourceRepoID, deploy[0].TargetRepoID)
	}
	platform := evidenceOfKind(evidence, EvidenceKindArgoCDDestinationPlatform)
	if len(platform) != 1 || platform[0].SourceRepoID != "repo-other" {
		t.Fatalf("platform evidence = %#v, want one fact for repo-other", platform)
	}
	if got := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource); len(got) != 0 {
		t.Fatalf("template-source evidence for a distinct service repo = %#v, want none", got)
	}
}

// TestApplicationSetSelfReferenceKeepsDiscoveryAndEmitsNoSelfLoop guards the
// reason the skip exists: the config repository names itself, and no evidence
// may run from a repository to itself.
func TestApplicationSetSelfReferenceKeepsDiscoveryAndEmitsNoSelfLoop(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
	}, appSetConfigRepoCatalog)

	discovery := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery)
	if len(discovery) != 1 || discovery[0].SourceRepoID != "repo-gitops" || discovery[0].TargetRepoID != "repo-app" {
		t.Fatalf("discovery evidence = %#v, want repo-gitops -> repo-app", discovery)
	}
	for _, fact := range evidence {
		if fact.SourceRepoID != "" && fact.SourceRepoID == fact.TargetRepoID {
			t.Fatalf("self-loop evidence %s %s -> %s", fact.EvidenceKind, fact.SourceRepoID, fact.TargetRepoID)
		}
	}
}

// TestApplicationSetControlRepoAsTemplateSourceEmitsNothing guards the second
// half of the skip: a template source that is the control repository is
// ignored, with and without the self-reference.
func TestApplicationSetControlRepoAsTemplateSourceEmitsNothing(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("central-config", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-central", "prod", "gitops-repo"),
	}, appSetConfigRepoCatalog)

	if deploy := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource); len(deploy) != 0 {
		t.Fatalf("deploy-source evidence for the control repo = %#v, want none", deploy)
	}
	if platform := evidenceOfKind(evidence, EvidenceKindArgoCDDestinationPlatform); len(platform) != 0 {
		t.Fatalf("platform evidence for the control repo = %#v, want none", platform)
	}
	if got := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource); len(got) != 0 {
		t.Fatalf("template-source evidence for the control repo = %#v, want none", got)
	}
}

// TestApplicationSetConfigOnlyRepoGetsNoDeployOrPlatformEvidence guards the
// accuracy risk of any fix: a repository that is only a config repository, and
// is not named as a template source, must not look deployed.
func TestApplicationSetConfigOnlyRepoGetsNoDeployOrPlatformEvidence(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("central-config", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-central", "prod", "other-service"),
	}, appSetConfigRepoCatalog)

	for _, fact := range evidence {
		if fact.EvidenceKind == EvidenceKindArgoCDApplicationSetDiscovery {
			continue
		}
		if fact.SourceRepoID == "repo-central" {
			t.Fatalf("config-only repo is the source of %s evidence: %#v", fact.EvidenceKind, fact)
		}
	}
}

// TestApplicationSetSelfReferenceTemplatedDestinationStaysSkipped guards that
// a templated destination never becomes platform evidence, in the self-reference
// shape too.
func TestApplicationSetSelfReferenceTemplatedDestinationStaysSkipped(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetTemplatedDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
	}, appSetConfigRepoCatalog)

	if platform := evidenceOfKind(evidence, EvidenceKindArgoCDDestinationPlatform); len(platform) != 0 {
		t.Fatalf("templated destination produced platform evidence: %#v", platform)
	}
}

// wantSelfReferenceFacts asserts the facts a self-referencing ApplicationSet
// (control repo-gitops, config repo and template source both repo-app) must
// produce. wantPlatform is the expected count of destination-platform facts.
func wantSelfReferenceFacts(t *testing.T, evidence []EvidenceFact, wantPlatform int) {
	t.Helper()

	discovery := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery)
	if len(discovery) != 1 || discovery[0].SourceRepoID != "repo-gitops" || discovery[0].TargetRepoID != "repo-app" {
		t.Fatalf("discovery evidence = %#v, want one repo-gitops -> repo-app", discovery)
	}
	if deploy := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource); len(deploy) != 0 {
		t.Fatalf("deploy-source evidence = %#v, want none (it would be a self-loop)", deploy)
	}
	template := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource)
	if len(template) != 1 {
		t.Fatalf("template-source evidence = %d, want 1: %#v", len(template), template)
	}
	fact := template[0]
	if fact.RelationshipType != RelDeploysFrom || fact.SourceRepoID != "repo-gitops" || fact.TargetRepoID != "repo-app" {
		t.Fatalf("template source = %s %s -> %s, want DEPLOYS_FROM repo-gitops -> repo-app",
			fact.RelationshipType, fact.SourceRepoID, fact.TargetRepoID)
	}
	want := DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDApplicationSetTemplateSource)
	if fact.Confidence != want || fact.Confidence != 0.95 {
		t.Fatalf("template source confidence = %v, registry = %v, want 0.95", fact.Confidence, want)
	}
	if got := fact.Details["control_plane_repo_id"]; got != "repo-gitops" {
		t.Fatalf("control_plane_repo_id = %#v, want repo-gitops", got)
	}
	if got := fact.Details["config_repo_id"]; got != "repo-app" {
		t.Fatalf("config_repo_id = %#v, want repo-app", got)
	}
	if got := fact.Details["path"]; got != "applicationsets/svc.yaml" {
		t.Fatalf("path = %#v, want applicationsets/svc.yaml", got)
	}
	for key, want := range map[string]any{
		"deploy_repo_url": "https://github.com/myorg/app-repo",
		"discovery_path":  "argocd/svc/overlays/*/config.yaml",
		"matched_alias":   "app-repo",
		"extractor":       "argocd",
		"self_reference":  true,
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
		if p.RelationshipType != RelRunsOn || p.SourceRepoID != "repo-app" ||
			p.TargetEntityID != "platform:kubernetes:none:server/kubernetes.default.svc:none:none" {
			t.Fatalf("platform fact = %#v, want RUNS_ON repo-app -> the default kubernetes platform", p)
		}
	}
	for _, f := range evidence {
		if f.SourceRepoID != "" && f.SourceRepoID == f.TargetRepoID {
			t.Fatalf("self-loop evidence %s %s -> %s", f.EvidenceKind, f.SourceRepoID, f.TargetRepoID)
		}
	}
}

// TestApplicationSetSelfReferenceEmitsControlToDeployedDeploysFrom is the
// #7767 fix: the config repository is also the template source, so the
// deployed repository is recorded as a DEPLOYS_FROM from the control
// repository and its platform is kept.
func TestApplicationSetSelfReferenceEmitsControlToDeployedDeploysFrom(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
	}, appSetConfigRepoCatalog)

	wantSelfReferenceFacts(t, evidence, 1)
}

// TestApplicationSetSelfReferenceManyOverlaysDeduplicates guards that several
// overlays naming the same repository collapse to one fact per ApplicationSet
// file.
func TestApplicationSetSelfReferenceManyOverlaysDeduplicates(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
		appSetConfigRepoConfig("repo-app", "staging", "app-repo"),
	}, appSetConfigRepoCatalog)

	wantSelfReferenceFacts(t, evidence, 1)
}

// TestApplicationSetSelfReferenceTemplatedDestinationStillDeploysFrom guards
// that the DEPLOYS_FROM edge does not depend on the destination.
func TestApplicationSetSelfReferenceTemplatedDestinationStillDeploysFrom(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetTemplatedDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
	}, appSetConfigRepoCatalog)

	wantSelfReferenceFacts(t, evidence, 0)
}

// TestApplicationSetSelfReferenceStructuredPathMatchesYAMLPath runs the
// self-reference through the parsed_file_data path and expects the same facts.
func TestApplicationSetSelfReferenceStructuredPathMatchesYAMLPath(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"artifact_type": "argocd",
			"relative_path": "applicationsets/svc.yaml",
			"parsed_file_data": map[string]any{
				"argocd_applicationsets": []any{
					map[string]any{
						"name":                   "svc",
						"generator_source_repos": "https://github.com/myorg/app-repo",
						"generator_source_paths": "argocd/svc/overlays/*/config.yaml",
						"template_source_repos":  "https://github.com/myorg/app-repo",
						"template_source_paths":  "deploy",
						"dest_server":            "https://kubernetes.default.svc",
						"dest_namespace":         "svc",
					},
				},
			},
		},
	}}, appSetConfigRepoCatalog)

	wantSelfReferenceFacts(t, evidence, 1)

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

// TestApplicationSetSelfReferenceResolvesToOneDeploysFrom runs the evidence
// through Resolve and expects one DEPLOYS_FROM control -> deployed edge that
// carries the new evidence kind, and one RUNS_ON.
func TestApplicationSetSelfReferenceResolvesToOneDeploysFrom(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetLiteralDestination),
		appSetConfigRepoConfig("repo-app", "prod", "app-repo"),
	}, appSetConfigRepoCatalog)

	_, resolved := Resolve(evidence, nil, 0)

	var deploys, runsOn int
	for _, relationship := range resolved {
		switch relationship.RelationshipType {
		case RelDeploysFrom:
			deploys++
			if relationship.SourceRepoID != "repo-gitops" || relationship.TargetRepoID != "repo-app" {
				t.Fatalf("DEPLOYS_FROM %s -> %s, want repo-gitops -> repo-app",
					relationship.SourceRepoID, relationship.TargetRepoID)
			}
			kinds, _ := relationship.Details["evidence_kinds"].([]string)
			if len(kinds) != 1 || kinds[0] != string(EvidenceKindArgoCDApplicationSetTemplateSource) {
				t.Fatalf("evidence_kinds = %#v, want [%s]", relationship.Details["evidence_kinds"],
					EvidenceKindArgoCDApplicationSetTemplateSource)
			}
		case RelRunsOn:
			runsOn++
		}
	}
	if deploys != 1 || runsOn != 1 {
		t.Fatalf("resolved DEPLOYS_FROM = %d, RUNS_ON = %d, want 1 and 1: %#v", deploys, runsOn, resolved)
	}
}
