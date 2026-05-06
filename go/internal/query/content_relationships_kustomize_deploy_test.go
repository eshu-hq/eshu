package query

import (
	"context"
	"testing"
)

func TestBuildContentRelationshipSetKustomizeOverlayPromotesTypedDeploySources(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, nil)
	reader := NewContentReader(db)
	overlay := EntityContent{
		EntityID:     "overlay-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/kustomization.yaml",
		EntityType:   "KustomizeOverlay",
		EntityName:   "kustomization",
		Language:     "yaml",
		Metadata: map[string]any{
			"resource_refs": "https://github.com/myorg/shared-manifests.git//payments?ref=main,shared/component",
			"helm_refs":     "https://charts.bitnami.com/bitnami,ingress-nginx,nginx",
			"image_refs":    "ghcr.io/example/nginx,nginx",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, overlay)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 7 {
		t.Fatalf("len(relationships.outgoing) = %d, want 7", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "https://github.com/myorg/shared-manifests.git//payments?ref=main"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "kustomize_resource_reference"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	helm := relationships.outgoing[2]
	if got, want := helm["reason"], "kustomize_helm_chart_reference"; got != want {
		t.Fatalf("relationships.outgoing[2][reason] = %#v, want %#v", got, want)
	}

	image := relationships.outgoing[5]
	if got, want := image["reason"], "kustomize_image_reference"; got != want {
		t.Fatalf("relationships.outgoing[5][reason] = %#v, want %#v", got, want)
	}
}
