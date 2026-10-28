// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// TestCov8JavaResolvers pins the Java binding onto the shared JVM
// receiver: an imported Service binds with import provenance, and the
// fallback gate mirrors the import attempt.
func TestCov8JavaResolvers(t *testing.T) {
	t.Parallel()

	resolvers := Resolvers()
	if len(resolvers) != 1 {
		t.Fatalf("len(Resolvers()) = %d, want 1", len(resolvers))
	}
	if resolvers[0].Phase != shared.PhaseBeforeRepoFallback {
		t.Errorf("Resolvers()[0].Phase = %v, want before-fallback", resolvers[0].Phase)
	}

	index := shared.BuildEntityIndex([]facts.Envelope{{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-java",
			"relative_path": "com/example/Service.java",
			"parsed_file_data": map[string]any{
				"path": "com/example/Service.java",
				"functions": []any{
					map[string]any{
						"name": "run", "uid": "entity-java-run",
						"class_context": "Service", "line_number": 3, "end_line": 5,
					},
				},
			},
		},
	}})
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-java",
		RelativePath: "com/example/App.java",
		RepositoryImports: map[string][]string{
			"Service": {"com/example/Service.java"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{
				"name": "com.example.Service", "source": "com.example.Service",
				"import_type": "import",
			},
		}},
		Call:     map[string]any{"name": "run", "inferred_obj_type": "Service"},
		Language: "java",
	}
	entityID, _, method := resolvers[0].Resolve(ctx)
	if entityID != "entity-java-run" || method != codeprovenance.MethodImportBinding {
		t.Errorf("Resolve() = %q, %q; want entity-java-run via import binding", entityID, method)
	}
	if !BlocksRepoFallback(ctx) {
		t.Error("BlocksRepoFallback(imported) = false, want true")
	}
	bare := ctx
	bare.FileData = map[string]any{}
	bare.RepositoryImports = nil
	if BlocksRepoFallback(bare) {
		t.Error("BlocksRepoFallback(no imports) = true, want false")
	}
}

// TestCov8MetadataFileRootCallerID pins the provider-reference caller
// identity: only the two metadata call kinds name the file root.
func TestCov8MetadataFileRootCallerID(t *testing.T) {
	t.Parallel()

	got := MetadataFileRootCallerID("repo-java", "META-INF/services/x",
		map[string]any{"call_kind": "java.service_loader_provider"})
	if got != "repo-java:META-INF/services/x" {
		t.Errorf("service_loader caller = %q, want repo:file", got)
	}
	got = MetadataFileRootCallerID("repo-java", "META-INF/spring.factories",
		map[string]any{"call_kind": "java.spring_autoconfiguration_class"})
	if got != "repo-java:META-INF/spring.factories" {
		t.Errorf("spring caller = %q, want repo:file", got)
	}
	for name, args := range map[string]struct {
		repo string
		path string
		call map[string]any
	}{
		"no repo":      {"", "a/b", map[string]any{"call_kind": "java.service_loader_provider"}},
		"no path":      {"repo-java", "", map[string]any{"call_kind": "java.service_loader_provider"}},
		"other kind":   {"repo-java", "a/b", map[string]any{"call_kind": "method_call"}},
		"missing kind": {"repo-java", "a/b", map[string]any{}},
	} {
		if got := MetadataFileRootCallerID(args.repo, args.path, args.call); got != "" {
			t.Errorf("callerID(%s) = %q, want empty", name, got)
		}
	}
}
