// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// appSetIdentityConfig is a per-environment config.yaml in the repository the
// ApplicationSet generator reads. Its git.repoURL names no catalog repository,
// so the only template source that can resolve to a catalog repository is the
// bare service identity under identityKey.
func appSetIdentityConfig(identityKey, identityValue string) facts.Envelope {
	return facts.Envelope{
		ScopeID: "repo-app",
		Payload: map[string]any{
			"artifact_type": "yaml",
			"relative_path": "argocd/svc/overlays/prod/config.yaml",
			"content": `helm:
  repoURL: registry.example.invalid
  chart: charts/svc
git:
  repoURL: https://example.invalid/unrelated
` + identityKey + `: ` + identityValue + `
`,
		},
	}
}

// TestApplicationSetIdentitySelfReferenceEmitsTemplateSource pins the fuzziest
// way to reach the self-reference branch: a bare identity value in the config
// file (here releaseName) that matches the config repository's own alias. The
// fact must carry the bare name in deploy_repo_url and the usual 0.95.
func TestApplicationSetIdentitySelfReferenceEmitsTemplateSource(t *testing.T) {
	t.Parallel()
	evidence := DiscoverEvidence([]facts.Envelope{
		appSetConfigRepoSet("app-repo", appSetLiteralDestination),
		appSetIdentityConfig("releaseName", "app-repo"),
	}, appSetConfigRepoCatalog)

	templateSource := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetTemplateSource)
	if len(templateSource) != 1 {
		t.Fatalf("template-source evidence = %d, want 1: %#v", len(templateSource), templateSource)
	}
	fact := templateSource[0]
	if fact.RelationshipType != RelDeploysFrom || fact.SourceRepoID != "repo-gitops" || fact.TargetRepoID != "repo-app" {
		t.Fatalf("fact = %s %s -> %s, want DEPLOYS_FROM repo-gitops -> repo-app", fact.RelationshipType, fact.SourceRepoID, fact.TargetRepoID)
	}
	if got := fact.Details["deploy_repo_url"]; got != "app-repo" {
		t.Fatalf("deploy_repo_url = %#v, want the bare identity name app-repo", got)
	}
	if fact.Confidence != 0.95 {
		t.Fatalf("confidence = %v, want 0.95", fact.Confidence)
	}
	if got := fact.Details["self_reference"]; got != true {
		t.Fatalf("self_reference = %#v, want true", got)
	}
	if deploy := evidenceOfKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource); len(deploy) != 0 {
		t.Fatalf("deploy-source evidence = %#v, want none (it would be a self-loop)", deploy)
	}
}

// TestApplicationSetBroadIdentityDoesNotEmitTemplateSource pins the guard on
// the same path: a broad bare identity (isBroadArgoServiceIdentity) must not
// become a deployment edge, even when the alias it would match exists.
func TestApplicationSetBroadIdentityDoesNotEmitTemplateSource(t *testing.T) {
	t.Parallel()
	// One catalog repository per broad value, so each row reaches the guard in
	// argocdServiceIdentityValues and fails if that guard is removed: "api" and
	// "app" are in the broad list, "web" is rejected only by the length rule.
	catalog := append([]CatalogEntry{
		{RepoID: "repo-api", Aliases: []string{"api"}},
		{RepoID: "repo-short-app", Aliases: []string{"app"}},
		{RepoID: "repo-web", Aliases: []string{"web"}},
	}, appSetConfigRepoCatalog...)
	for _, tc := range []struct{ key, value string }{
		{"name", "api"},
		{"app", "app"},
		{"service", "web"},
	} {
		evidence := DiscoverEvidence([]facts.Envelope{
			appSetConfigRepoSet("app-repo", appSetLiteralDestination),
			appSetIdentityConfig(tc.key, tc.value),
		}, catalog)
		for _, kind := range []EvidenceKind{
			EvidenceKindArgoCDApplicationSetTemplateSource,
			EvidenceKindArgoCDApplicationSetDeploySource,
		} {
			if got := evidenceOfKind(evidence, kind); len(got) != 0 {
				t.Fatalf("%s: %s = %#v, want none for a broad identity", tc.key+"="+tc.value, kind, got)
			}
		}
	}
}
