package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestApplyLocalGraphBootstrapAppliesNornicDBSchema(t *testing.T) {
	originalApplySchema := localGraphApplySchema
	t.Cleanup(func() {
		localGraphApplySchema = originalApplySchema
	})

	var gotBackend graph.SchemaBackend
	var gotURI string
	var gotBackendEnv string
	var called bool
	localGraphApplySchema = func(ctx context.Context, getenv func(string) string, backend graph.SchemaBackend) error {
		called = true
		gotBackend = backend
		gotURI = getenv("ESHU_NEO4J_URI")
		gotBackendEnv = getenv("ESHU_GRAPH_BACKEND")
		if getenv("ESHU_NEO4J_DATABASE") != localNornicDBDefaultDatabase {
			t.Fatalf("ESHU_NEO4J_DATABASE = %q, want %q", getenv("ESHU_NEO4J_DATABASE"), localNornicDBDefaultDatabase)
		}
		return nil
	}

	err := applyLocalGraphBootstrap(
		context.Background(),
		localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
		&managedLocalGraph{
			Backend:  query.GraphBackendNornicDB,
			Address:  "127.0.0.1",
			BoltPort: 17687,
			Username: "admin",
			Password: "workspace-secret",
		},
	)
	if err != nil {
		t.Fatalf("applyLocalGraphBootstrap() error = %v, want nil", err)
	}
	if !called {
		t.Fatal("applyLocalGraphBootstrap() did not apply graph schema")
	}
	if gotBackend != graph.SchemaBackendNornicDB {
		t.Fatalf("schema backend = %q, want %q", gotBackend, graph.SchemaBackendNornicDB)
	}
	if gotURI != "bolt://127.0.0.1:17687" {
		t.Fatalf("ESHU_NEO4J_URI = %q, want bolt://127.0.0.1:17687", gotURI)
	}
	if gotBackendEnv != string(query.GraphBackendNornicDB) {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want %q", gotBackendEnv, query.GraphBackendNornicDB)
	}
}

func TestApplyLocalGraphBootstrapRejectsBackendMismatch(t *testing.T) {
	err := applyLocalGraphBootstrap(
		context.Background(),
		localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
		&managedLocalGraph{Backend: query.GraphBackendNeo4j},
	)
	if err == nil || !strings.Contains(err.Error(), "backend mismatch") {
		t.Fatalf("applyLocalGraphBootstrap() error = %v, want backend mismatch", err)
	}
}

func TestApplyLocalGraphBootstrapSkipsLightweightProfile(t *testing.T) {
	originalApplySchema := localGraphApplySchema
	t.Cleanup(func() {
		localGraphApplySchema = originalApplySchema
	})

	localGraphApplySchema = func(ctx context.Context, getenv func(string) string, backend graph.SchemaBackend) error {
		t.Fatal("localGraphApplySchema called for lightweight profile")
		return nil
	}

	err := applyLocalGraphBootstrap(
		context.Background(),
		localHostRuntimeConfig{Profile: query.ProfileLocalLightweight},
		nil,
	)
	if err != nil {
		t.Fatalf("applyLocalGraphBootstrap() error = %v, want nil", err)
	}
}
