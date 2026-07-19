// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestBuildContentRelationshipSetK8sServiceSelectsDifferentlyNamedDeploymentBySelector
// proves the false-negative fix: a Service with a known selector SELECTS a
// Deployment with a DIFFERENT name whose pod-template labels satisfy the
// selector, with reason k8s_service_selector_match. Today, before this fix,
// this produced no edge at all because the old query-time heuristic only
// ever matched by identical name+namespace. See #5343.
func TestBuildContentRelationshipSetK8sServiceSelectsDifferentlyNamedDeploymentBySelector(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/frontend.yaml", "K8sResource", "frontend-deploy",
					int64(1), int64(20), "yaml", "kind: Deployment",
					[]byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/frontend-deploy","pod_template_labels":"app=frontend,tier=web"}`),
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
		EntityName:   "web",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1: %#v", len(relationships.outgoing), relationships.outgoing)
	}

	relationship := relationships.outgoing[0]
	if got, want := relationship["target_name"], "frontend-deploy"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "k8s_service_selector_match"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

// TestBuildContentRelationshipSetK8sAnchorSelectorMismatchNeverFallsBackToName
// is the load-bearing anchor: a Service and Deployment sharing the SAME name
// and namespace but with a KNOWN, non-matching selector must produce NO
// SELECTS edge of any reason. Before this fix, the name+namespace heuristic
// alone produced a false-positive edge here. This also proves the fallback
// path is structurally unreachable once the selector is known.
func TestBuildContentRelationshipSetK8sAnchorSelectorMismatchNeverFallsBackToName(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/api.yaml", "K8sResource", "api",
					int64(1), int64(20), "yaml", "kind: Deployment",
					[]byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/api","pod_template_labels":"app=api-v1"}`),
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
		EntityName:   "api",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/api",
			"selector":       "app=api-v2",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0 (no edge of any reason): %#v", len(relationships.outgoing), relationships.outgoing)
	}
}

// TestBuildContentRelationshipSetK8sSelectorlessServiceProducesNoEdge proves
// a Service with a known, EMPTY selector (ExternalName/manual Endpoints)
// never SELECTS anything, even a same-named/same-namespace Deployment -- the
// empty-selector-vacuous-subset guard applied end to end.
func TestBuildContentRelationshipSetK8sSelectorlessServiceProducesNoEdge(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/external.yaml", "K8sResource", "external",
					int64(1), int64(20), "yaml", "kind: Deployment",
					[]byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/external","pod_template_labels":"app=external"}`),
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
		EntityName:   "external",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/external",
			"selector":       "",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0: %#v", len(relationships.outgoing), relationships.outgoing)
	}
}

// TestBuildContentRelationshipSetK8sServiceSelectsMultipleDeployments proves
// one Service selector matching multiple Deployments emits an edge for each.
func TestBuildContentRelationshipSetK8sServiceSelectsMultipleDeployments(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/blue.yaml", "K8sResource", "api-blue",
					int64(1), int64(20), "yaml", "kind: Deployment",
					[]byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/api-blue","pod_template_labels":"app=api,track=blue"}`),
				},
				{
					"deployment-2", "repo-1", "deploy/green.yaml", "K8sResource", "api-green",
					int64(1), int64(20), "yaml", "kind: Deployment",
					[]byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/api-green","pod_template_labels":"app=api,track=green"}`),
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
		EntityName:   "api",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/api",
			"selector":       "app=api",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2: %#v", len(relationships.outgoing), relationships.outgoing)
	}
	targets := map[string]bool{}
	for _, relationship := range relationships.outgoing {
		targets[relationship["target_name"].(string)] = true
		if got, want := relationship["reason"], "k8s_service_selector_match"; got != want {
			t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
		}
	}
	if !targets["api-blue"] || !targets["api-green"] {
		t.Fatalf("targets = %#v, want both api-blue and api-green", targets)
	}
}

// TestBuildContentRelationshipSetK8sSelectorMatchNamespaceScoped proves a
// matching selector across different namespaces produces no edge on the
// content_relationships path.
func TestBuildContentRelationshipSetK8sSelectorMatchNamespaceScoped(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/frontend.yaml", "K8sResource", "frontend-deploy",
					int64(1), int64(20), "yaml", "kind: Deployment",
					[]byte(`{"kind":"Deployment","namespace":"staging","qualified_name":"staging/Deployment/frontend-deploy","pod_template_labels":"app=frontend"}`),
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
		EntityName:   "web",
		Language:     "yaml",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, service)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0 (namespace mismatch): %#v", len(relationships.outgoing), relationships.outgoing)
	}
}
