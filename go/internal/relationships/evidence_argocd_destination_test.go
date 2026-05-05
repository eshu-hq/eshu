package relationships

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverArgoCDApplicationDestinationPlatformEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"artifact_type": "argocd",
			"relative_path": "apps/payments.yaml",
			"content": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  destination:
    name: prod-cluster
    namespace: payments
  source:
    repoURL: https://github.com/myorg/payments-service.git
`,
		},
	}}
	catalog := []CatalogEntry{{RepoID: "repo-payments", Aliases: []string{"payments-service"}}}

	evidence := DiscoverEvidence(envelopes, catalog)
	fact, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDDestinationPlatform)
	if !ok {
		t.Fatal("missing ARGOCD_DESTINATION_PLATFORM evidence")
	}
	if fact.RelationshipType != RelRunsOn {
		t.Fatalf("relationship type = %q, want %q", fact.RelationshipType, RelRunsOn)
	}
	if fact.SourceRepoID != "repo-payments" {
		t.Fatalf("source repo = %q, want repo-payments", fact.SourceRepoID)
	}
	if !strings.Contains(fact.TargetEntityID, "prod-cluster") {
		t.Fatalf("target entity = %q, want platform id containing prod-cluster", fact.TargetEntityID)
	}
}

func TestDiscoverArgoCDApplicationSetDestinationPlatformEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"artifact_type": "argocd",
			"relative_path": "apps/applicationset.yaml",
			"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/payments-config.git
        files:
          - path: argocd/*/config.yaml
  template:
    spec:
      destination:
        server: https://kubernetes.default.svc
        namespace: payments
      source:
        repoURL: https://github.com/myorg/payments-service.git
`,
		},
	}}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"payments-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	fact, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDDestinationPlatform)
	if !ok {
		t.Fatal("missing ARGOCD_DESTINATION_PLATFORM evidence")
	}
	if fact.RelationshipType != RelRunsOn {
		t.Fatalf("relationship type = %q, want %q", fact.RelationshipType, RelRunsOn)
	}
	if fact.SourceRepoID != "repo-payments" {
		t.Fatalf("source repo = %q, want repo-payments", fact.SourceRepoID)
	}
	if !strings.Contains(fact.TargetEntityID, "kubernetes.default.svc") {
		t.Fatalf("target entity = %q, want platform id containing kubernetes.default.svc", fact.TargetEntityID)
	}
}

func TestDiscoverArgoCDApplicationSetSkipsTemplatedDestinationPlatform(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/applicationset.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: services/*/config.yaml
  template:
    spec:
      source:
        repoURL: https://github.com/myorg/payments-service.git
      destination:
        server: "{{.server}}"
        namespace: "{{.namespace}}"
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if _, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDDestinationPlatform); ok {
		t.Fatalf("unexpected templated destination platform evidence: %#v", evidence)
	}
}

func findEvidenceByKind(evidence []EvidenceFact, want EvidenceKind) (EvidenceFact, bool) {
	for _, fact := range evidence {
		if fact.EvidenceKind == want {
			return fact, true
		}
	}
	return EvidenceFact{}, false
}
