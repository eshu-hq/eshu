package query

import (
	"context"
	"testing"
)

func TestBuildContentRelationshipSetDockerfilePromotesSourceLabel(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "dockerfile-1",
		RepoID:       "repo-runtime",
		RelativePath: "Dockerfile",
		EntityType:   "File",
		Language:     "dockerfile",
		SourceCache: `FROM alpine:3.20
LABEL org.opencontainers.image.source="https://github.com/acme/payments-service.git"
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "https://github.com/acme/payments-service.git"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "dockerfile_source_label"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
