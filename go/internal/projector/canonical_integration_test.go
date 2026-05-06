package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func makeTestScope(scopeID, repoID, repoPath string) (scope.IngestionScope, scope.ScopeGeneration) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repoID,
		Metadata: map[string]string{
			"repo_id":   repoID,
			"repo_path": repoPath,
		},
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: scopeID + "-gen",
		ScopeID:      scopeID,
		ObservedAt:   now,
		IngestedAt:   now.Add(5 * time.Minute),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return scopeValue, generationValue
}

func TestCanonicalProjectionEndToEndWritesAllStages(t *testing.T) {
	t.Parallel()

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{result: content.Result{RecordCount: 2, EntityCount: 2}}
	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
	}

	repoID := "repo-integration-1"
	repoPath := "org/integration-test-repo"
	scopeValue, generationValue := makeTestScope("scope-integration-123", repoID, repoPath)
	now := generationValue.ObservedAt

	inputFacts := []facts.Envelope{
		{
			FactID:       "fact-repo",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "repository",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":    repoID,
				"name":       "integration-test-repo",
				"path":       repoPath,
				"local_path": "/tmp/repos/org/integration-test-repo",
				"remote_url": "https://github.com/org/integration-test-repo.git",
				"repo_slug":  "org/integration-test-repo",
				"has_remote": true,
			},
		},
		{
			FactID:       "fact-file-1",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "file",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"path":          repoPath + "/src/main.go",
				"relative_path": "src/main.go",
				"name":          "main.go",
				"language":      "go",
			},
		},
		{
			FactID:       "fact-file-2",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "file",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"path":          repoPath + "/src/api/handler.go",
				"relative_path": "src/api/handler.go",
				"name":          "handler.go",
				"language":      "go",
			},
		},
		{
			FactID:       "fact-entity-1",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "content_entity",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"entity_id":     "entity-func-1",
				"entity_type":   "function",
				"entity_name":   "handleRequest",
				"relative_path": "src/api/handler.go",
				"start_line":    float64(10),
				"end_line":      float64(25),
				"language":      "go",
			},
		},
		{
			FactID:       "fact-entity-2",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "content_entity",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"entity_id":     "entity-class-1",
				"entity_type":   "class",
				"entity_name":   "Server",
				"relative_path": "src/main.go",
				"start_line":    float64(5),
				"end_line":      float64(50),
				"language":      "go",
			},
		},
	}

	result, err := runtime.Project(context.Background(), scopeValue, generationValue, inputFacts)
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}

	if got, want := len(canonicalWriter.calls), 1; got != want {
		t.Fatalf("CanonicalWriter.Write call count = %d, want %d", got, want)
	}

	mat := canonicalWriter.calls[0]

	if got, want := mat.ScopeID, scopeValue.ScopeID; got != want {
		t.Errorf("mat.ScopeID = %q, want %q", got, want)
	}
	if got, want := mat.GenerationID, generationValue.GenerationID; got != want {
		t.Errorf("mat.GenerationID = %q, want %q", got, want)
	}

	if mat.Repository == nil {
		t.Fatal("mat.Repository = nil, want non-nil")
	}
	if got, want := mat.Repository.RepoID, repoID; got != want {
		t.Errorf("mat.Repository.RepoID = %q, want %q", got, want)
	}
	if got, want := mat.Repository.Name, "integration-test-repo"; got != want {
		t.Errorf("mat.Repository.Name = %q, want %q", got, want)
	}
	if got, want := mat.Repository.HasRemote, true; got != want {
		t.Errorf("mat.Repository.HasRemote = %v, want %v", got, want)
	}

	if got, want := len(mat.Directories), 2; got != want {
		t.Fatalf("len(mat.Directories) = %d, want %d", got, want)
	}
	if got, want := mat.Directories[0].Depth, 0; got != want {
		t.Errorf("mat.Directories[0].Depth = %d, want %d", got, want)
	}
	if got, want := mat.Directories[0].Name, "src"; got != want {
		t.Errorf("mat.Directories[0].Name = %q, want %q", got, want)
	}
	if got, want := mat.Directories[1].Depth, 1; got != want {
		t.Errorf("mat.Directories[1].Depth = %d, want %d", got, want)
	}
	if got, want := mat.Directories[1].Name, "api"; got != want {
		t.Errorf("mat.Directories[1].Name = %q, want %q", got, want)
	}

	if got, want := len(mat.Files), 2; got != want {
		t.Fatalf("len(mat.Files) = %d, want %d", got, want)
	}

	if got, want := len(mat.Entities), 2; got != want {
		t.Fatalf("len(mat.Entities) = %d, want %d", got, want)
	}

	entityLabels := make(map[string]string)
	for _, e := range mat.Entities {
		entityLabels[e.EntityName] = e.Label
	}
	if got, want := entityLabels["handleRequest"], "Function"; got != want {
		t.Errorf("handleRequest label = %q, want %q", got, want)
	}
	if got, want := entityLabels["Server"], "Class"; got != want {
		t.Errorf("Server label = %q, want %q", got, want)
	}

	if got, want := len(contentWriter.calls), 1; got != want {
		t.Fatalf("ContentWriter.Write call count = %d, want %d", got, want)
	}
	if got, want := result.Content.RecordCount, 2; got != want {
		t.Errorf("result.Content.RecordCount = %d, want %d", got, want)
	}
	if got, want := result.Content.EntityCount, 2; got != want {
		t.Errorf("result.Content.EntityCount = %d, want %d", got, want)
	}
}

func TestCanonicalProjectionSkipsWriteOnEmptyFacts(t *testing.T) {
	t.Parallel()

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{result: content.Result{RecordCount: 0}}
	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
	}

	scopeValue, generationValue := makeTestScope("scope-empty-123", "repo-empty", "org/empty")
	now := generationValue.ObservedAt

	inputFacts := []facts.Envelope{
		{
			FactID:       "fact-content-1",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "source_content",
			ObservedAt:   now,
			Payload: map[string]any{
				"content_path": "README.md",
				"content_body": "# Test",
			},
		},
	}

	_, err := runtime.Project(context.Background(), scopeValue, generationValue, inputFacts)
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}

	// Verify CanonicalWriter.Write was NOT called (empty materialization gates it).
	if got, want := len(canonicalWriter.calls), 0; got != want {
		t.Errorf("CanonicalWriter.Write call count = %d, want %d (should skip on empty canonical)", got, want)
	}

	// Verify ContentWriter IS still called.
	if got, want := len(contentWriter.calls), 1; got != want {
		t.Errorf("ContentWriter.Write call count = %d, want %d", got, want)
	}
}

func TestCanonicalProjectionDirectoryChainOrdering(t *testing.T) {
	t.Parallel()

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{result: content.Result{RecordCount: 3}}
	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
	}

	repoID := "repo-dir-1"
	repoPath := "org/test-repo"
	scopeValue, generationValue := makeTestScope("scope-dir-123", repoID, repoPath)
	now := generationValue.ObservedAt

	inputFacts := []facts.Envelope{
		{
			FactID:       "fact-repo",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "repository",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id": repoID,
				"name":    "test-repo",
				"path":    repoPath,
			},
		},
		{
			FactID:       "fact-file-1",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "file",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"path":          repoPath + "/src/main.go",
				"relative_path": "src/main.go",
				"name":          "main.go",
				"language":      "go",
			},
		},
		{
			FactID:       "fact-file-2",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "file",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"path":          repoPath + "/src/pkg/util.go",
				"relative_path": "src/pkg/util.go",
				"name":          "util.go",
				"language":      "go",
			},
		},
		{
			FactID:       "fact-file-3",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "file",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"path":          repoPath + "/src/pkg/internal/helpers.go",
				"relative_path": "src/pkg/internal/helpers.go",
				"name":          "helpers.go",
				"language":      "go",
			},
		},
	}

	_, err := runtime.Project(context.Background(), scopeValue, generationValue, inputFacts)
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}

	if got, want := len(canonicalWriter.calls), 1; got != want {
		t.Fatalf("CanonicalWriter.Write call count = %d, want %d", got, want)
	}

	mat := canonicalWriter.calls[0]

	if got, want := len(mat.Directories), 3; got != want {
		t.Fatalf("len(mat.Directories) = %d, want %d", got, want)
	}

	if got, want := mat.Directories[0].Depth, 0; got != want {
		t.Errorf("mat.Directories[0].Depth = %d, want %d", got, want)
	}
	if got, want := mat.Directories[1].Depth, 1; got != want {
		t.Errorf("mat.Directories[1].Depth = %d, want %d", got, want)
	}
	if got, want := mat.Directories[2].Depth, 2; got != want {
		t.Errorf("mat.Directories[2].Depth = %d, want %d", got, want)
	}

	if got, want := mat.Directories[0].ParentPath, "org/test-repo"; got != want {
		t.Errorf("mat.Directories[0].ParentPath = %q, want %q", got, want)
	}
	if got, want := mat.Directories[1].ParentPath, mat.Directories[0].Path; got != want {
		t.Errorf("mat.Directories[1].ParentPath = %q, want %q", got, want)
	}
	if got, want := mat.Directories[2].ParentPath, mat.Directories[1].Path; got != want {
		t.Errorf("mat.Directories[2].ParentPath = %q, want %q", got, want)
	}
}

func TestCanonicalProjectionEntityLabelMapping(t *testing.T) {
	t.Parallel()

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{result: content.Result{EntityCount: 6}}
	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
	}

	repoID := "repo-labels-1"
	repoPath := "org/test-repo"
	scopeValue, generationValue := makeTestScope("scope-labels-123", repoID, repoPath)
	now := generationValue.ObservedAt

	entityTypes := []struct {
		id, typ, name, path string
	}{
		{"entity-function-1", "function", "handleRequest", "src/handler.go"},
		{"entity-class-1", "class", "Server", "src/server.go"},
		{"entity-interface-1", "interface", "Handler", "src/handler.go"},
		{"entity-struct-1", "struct", "Config", "src/config.go"},
		{"entity-k8s-1", "k8s_resource", "my-deployment", "k8s/deployment.yaml"},
		{"entity-terraform-1", "terraform_resource", "aws_instance.web", "terraform/main.tf"},
	}

	inputFacts := []facts.Envelope{
		{
			FactID:       "fact-repo",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "repository",
			ObservedAt:   now,
			Payload:      map[string]any{"repo_id": repoID, "name": "test-repo", "path": repoPath},
		},
	}

	for _, e := range entityTypes {
		inputFacts = append(inputFacts, facts.Envelope{
			FactID:       "fact-" + e.id,
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generationValue.GenerationID,
			FactKind:     "content_entity",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id": repoID, "entity_id": e.id, "entity_type": e.typ,
				"entity_name": e.name, "relative_path": e.path, "start_line": float64(10),
				"end_line": float64(20), "language": "go",
			},
		})
	}

	_, err := runtime.Project(context.Background(), scopeValue, generationValue, inputFacts)
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}

	if got, want := len(canonicalWriter.calls), 1; got != want {
		t.Fatalf("CanonicalWriter.Write call count = %d, want %d", got, want)
	}

	mat := canonicalWriter.calls[0]

	if got, want := len(mat.Entities), 6; got != want {
		t.Fatalf("len(mat.Entities) = %d, want %d", got, want)
	}

	expectedLabels := map[string]string{
		"handleRequest":    "Function",
		"Server":           "Class",
		"Handler":          "Interface",
		"Config":           "Struct",
		"my-deployment":    "K8sResource",
		"aws_instance.web": "TerraformResource",
	}

	for _, entity := range mat.Entities {
		expectedLabel, ok := expectedLabels[entity.EntityName]
		if !ok {
			t.Errorf("unexpected entity %q in mat.Entities", entity.EntityName)
			continue
		}
		if got, want := entity.Label, expectedLabel; got != want {
			t.Errorf("entity %q Label = %q, want %q", entity.EntityName, got, want)
		}
	}
}
