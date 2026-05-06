package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestBuildContentRelationshipSetKustomizeOverlayPatchesTargetResource(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"k8s-resource-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "comprehensive-app",
					int64(1), int64(18), "yaml", "kind: Deployment", []byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/comprehensive-app"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	overlay := EntityContent{
		EntityID:     "overlay-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/kustomization.yaml",
		EntityType:   "KustomizeOverlay",
		EntityName:   "kustomization",
		Language:     "yaml",
		Metadata: map[string]any{
			"patch_targets": []string{"Deployment/comprehensive-app"},
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, overlay)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "PATCHES"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "comprehensive-app"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_id"], "k8s-resource-1"; got != want {
		t.Fatalf("relationship[target_id] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "kustomize_patch_target"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
