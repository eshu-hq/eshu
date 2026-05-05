package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverDockerComposeEvidencePromotesBuildContextAndImageReferences(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"artifact_type": "docker_compose",
				"relative_path": "docker-compose.yaml",
				"content": `services:
  payments:
    build:
      context: ../payments-service
    depends_on:
      - checkout-service
  checkout:
    image: ghcr.io/myorg/checkout-service:latest
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
		{RepoID: "repo-checkout", Aliases: []string{"checkout-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 3 {
		t.Fatalf("len = %d, want 3", len(evidence))
	}

	got := make(map[EvidenceKind]EvidenceFact, len(evidence))
	for _, fact := range evidence {
		got[fact.EvidenceKind] = fact
	}

	buildContext, ok := got[EvidenceKindDockerComposeBuildContext]
	if !ok {
		t.Fatal("missing docker compose build-context evidence")
	}
	if got, want := buildContext.TargetRepoID, "repo-payments"; got != want {
		t.Fatalf("build context target = %q, want %q", got, want)
	}
	if got, want := buildContext.Details["build_context"], "../payments-service"; got != want {
		t.Fatalf("build context details = %#v, want %#v", got, want)
	}
	if got, want := buildContext.RelationshipType, RelDeploysFrom; got != want {
		t.Fatalf("build context relationship = %q, want %q", got, want)
	}

	imageRef, ok := got[EvidenceKindDockerComposeImage]
	if !ok {
		t.Fatal("missing docker compose image evidence")
	}
	if got, want := imageRef.TargetRepoID, "repo-checkout"; got != want {
		t.Fatalf("image ref target = %q, want %q", got, want)
	}
	if got, want := imageRef.Details["image_ref"], "ghcr.io/myorg/checkout-service:latest"; got != want {
		t.Fatalf("image ref details = %#v, want %#v", got, want)
	}
	if got, want := imageRef.RelationshipType, RelDeploysFrom; got != want {
		t.Fatalf("image ref relationship = %q, want %q", got, want)
	}

	dependsOn, ok := got[EvidenceKindDockerComposeDependsOn]
	if !ok {
		t.Fatal("missing docker compose depends_on evidence")
	}
	if got, want := dependsOn.TargetRepoID, "repo-checkout"; got != want {
		t.Fatalf("depends_on target = %q, want %q", got, want)
	}
	if got, want := dependsOn.RelationshipType, RelDependsOn; got != want {
		t.Fatalf("depends_on relationship = %q, want %q", got, want)
	}
	if got, want := dependsOn.Details["depends_on_service"], "checkout-service"; got != want {
		t.Fatalf("depends_on details = %#v, want %#v", got, want)
	}
}

func TestDiscoverDockerComposeEvidenceSupportsBuildShorthandContext(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"artifact_type": "docker_compose",
				"relative_path": "docker-compose.yaml",
				"content": `services:
  api:
    build: ../payments-service
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}

	buildContext := evidence[0]
	if got, want := buildContext.EvidenceKind, EvidenceKindDockerComposeBuildContext; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
	if got, want := buildContext.TargetRepoID, "repo-payments"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if got, want := buildContext.Details["build_context"], "../payments-service"; got != want {
		t.Fatalf("details = %#v, want %#v", got, want)
	}
}
