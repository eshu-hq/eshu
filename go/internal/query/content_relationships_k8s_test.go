package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestBuildContentRelationshipSetK8sServiceSelectsDeployment(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "demo",
					int64(1), int64(20), "yaml", "kind: Deployment", []byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/demo"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "demo",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/demo",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "SELECTS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "demo"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_id"], "deployment-1"; got != want {
		t.Fatalf("relationship[target_id] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "k8s_service_name_namespace"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetK8sDeploymentReceivesIncomingServiceSelects(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"service-1", "repo-1", "deploy/service.yaml", "K8sResource", "demo",
					int64(1), int64(14), "yaml", "kind: Service", []byte(`{"kind":"Service","namespace":"prod","qualified_name":"prod/Service/demo"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	deployment := EntityContent{
		EntityID:     "deployment-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/deployment.yaml",
		EntityType:   "K8sResource",
		EntityName:   "demo",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Deployment",
			"namespace":      "prod",
			"qualified_name": "prod/Deployment/demo",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, deployment)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.incoming) != 1 {
		t.Fatalf("len(relationships.incoming) = %d, want 1", len(relationships.incoming))
	}

	relationship := relationships.incoming[0]
	if got, want := relationship["type"], "SELECTS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_name"], "demo"; got != want {
		t.Fatalf("relationship[source_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_id"], "service-1"; got != want {
		t.Fatalf("relationship[source_id] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "k8s_service_name_namespace"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
