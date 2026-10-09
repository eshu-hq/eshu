// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// readArgoCDComprehensiveFixture reads one file of the public synthetic
// tests/fixtures/ecosystems/argocd_comprehensive fixture from disk.
func readArgoCDComprehensiveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append(
		[]string{"..", "..", "..", "tests", "fixtures", "ecosystems", "argocd_comprehensive"},
		parts...,
	)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return string(content)
}

// TestArgoCDSelfConfigServiceFixtureEmitsControlToServiceDeploysFrom loads the
// real-world #7767 shape from disk: an ApplicationSet in a control repository
// whose git generator reads config from a service repository that names itself
// in its own config.yaml, with an OCI Helm chart and a values reference of
// {{ .git.repoURL }}. The service repository must be recorded as a DEPLOYS_FROM
// from the control repository, with its platform, and never as a self-loop.
func TestArgoCDSelfConfigServiceFixtureEmitsControlToServiceDeploysFrom(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "applicationsets/self-config-service.yaml",
				"content":       readArgoCDComprehensiveFixture(t, "applicationsets", "self-config-service.yaml"),
			},
		},
		{
			ScopeID: "repo-payments",
			Payload: map[string]any{
				"artifact_type": "yaml",
				"relative_path": "argocd/payments/overlays/prod/config.yaml",
				"content": readArgoCDComprehensiveFixture(t,
					"self-config-service", "argocd", "payments", "overlays", "prod", "config.yaml"),
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
		{RepoID: "repo-gitops", Aliases: []string{"gitops-repo"}},
	}

	evidence, stats := DiscoverEvidenceWithStats(envelopes, catalog)

	discovery := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery)
	if len(discovery) != 1 || discovery[0].SourceRepoID != "repo-gitops" || discovery[0].TargetRepoID != "repo-payments" {
		t.Fatalf("discovery = %#v, want one repo-gitops -> repo-payments", discovery)
	}
	if got := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource); len(got) != 0 {
		t.Fatalf("deploy-source = %#v, want none (it would be a self-loop)", got)
	}
	template := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource)
	if len(template) != 1 || template[0].RelationshipType != RelDeploysFrom ||
		template[0].SourceRepoID != "repo-gitops" || template[0].TargetRepoID != "repo-payments" ||
		template[0].Confidence != 0.95 {
		t.Fatalf("template-source = %#v, want one DEPLOYS_FROM repo-gitops -> repo-payments at 0.95", template)
	}
	platform := evidenceOfKind(evidence, EvidenceKindArgoCDDestinationPlatform)
	if len(platform) != 1 || platform[0].SourceRepoID != "repo-payments" ||
		platform[0].TargetEntityID != "platform:kubernetes:none:server/kubernetes.default.svc:none:none" {
		t.Fatalf("platform = %#v, want one RUNS_ON repo-payments -> kubernetes.default.svc", platform)
	}
	for _, fact := range evidence {
		if fact.SourceRepoID != "" && fact.SourceRepoID == fact.TargetRepoID {
			t.Fatalf("self-loop evidence %s %s -> %s", fact.EvidenceKind, fact.SourceRepoID, fact.TargetRepoID)
		}
	}
	if want := (ApplicationSetTemplateSourceStats{SelfReference: 1}); stats.ApplicationSetTemplateSource != want {
		t.Fatalf("stats = %+v, want %+v", stats.ApplicationSetTemplateSource, want)
	}
}
