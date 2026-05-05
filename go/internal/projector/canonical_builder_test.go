package projector

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func testScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-1",
		SourceSystem:  "test",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "part-1",
		Metadata: map[string]string{
			"repo_id":   "repo-abc",
			"repo_path": "/repos/my-project",
		},
	}
}

func testGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "gen-1",
		ScopeID:      "scope-1",
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func TestBuildCanonicalMaterializationExtractsRepository(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":    "repo-abc",
				"name":       "my-project",
				"path":       "/repos/my-project",
				"local_path": "/home/user/repos/my-project",
				"remote_url": "https://github.com/org/my-project.git",
				"repo_slug":  "org/my-project",
				"has_remote": "true",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if result.ScopeID != "scope-1" {
		t.Errorf("ScopeID = %q, want %q", result.ScopeID, "scope-1")
	}
	if result.GenerationID != "gen-1" {
		t.Errorf("GenerationID = %q, want %q", result.GenerationID, "gen-1")
	}
	if result.Repository == nil {
		t.Fatal("Repository is nil")
	}
	repo := result.Repository
	if repo.RepoID != "repo-abc" {
		t.Errorf("RepoID = %q, want %q", repo.RepoID, "repo-abc")
	}
	if repo.Name != "my-project" {
		t.Errorf("Name = %q, want %q", repo.Name, "my-project")
	}
	if repo.Path != "/repos/my-project" {
		t.Errorf("Path = %q, want %q", repo.Path, "/repos/my-project")
	}
	if repo.LocalPath != "/home/user/repos/my-project" {
		t.Errorf("LocalPath = %q, want %q", repo.LocalPath, "/home/user/repos/my-project")
	}
	if repo.RemoteURL != "https://github.com/org/my-project.git" {
		t.Errorf("RemoteURL = %q", repo.RemoteURL)
	}
	if repo.RepoSlug != "org/my-project" {
		t.Errorf("RepoSlug = %q", repo.RepoSlug)
	}
	if !repo.HasRemote {
		t.Error("HasRemote = false, want true")
	}
	if result.RepoPath != "/repos/my-project" {
		t.Errorf("RepoPath = %q, want %q", result.RepoPath, "/repos/my-project")
	}
	if result.RepoID != "repo-abc" {
		t.Errorf("RepoID = %q, want %q", result.RepoID, "repo-abc")
	}
}

func TestBuildCanonicalMaterializationMarksFirstGenerationFromScope(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()

	result := buildCanonicalMaterialization(sc, gen, nil)
	if !result.FirstGeneration {
		t.Fatal("FirstGeneration = false, want true when scope has no previous generation")
	}

	sc.ActiveGenerationID = "gen-previous"
	sc.PreviousGenerationExists = true
	result = buildCanonicalMaterialization(sc, gen, nil)
	if result.FirstGeneration {
		t.Fatal("FirstGeneration = true, want false when scope has a prior generation")
	}
}

func TestBuildCanonicalMaterializationDoesNotInferPriorGenerationFromActiveGenerationID(t *testing.T) {
	t.Parallel()

	sc := testScope()
	sc.ActiveGenerationID = "gen-active-from-fixture"
	gen := testGeneration()

	result := buildCanonicalMaterialization(sc, gen, nil)
	if !result.FirstGeneration {
		t.Fatal("FirstGeneration = false, want true unless PreviousGenerationExists is explicit")
	}
}

func TestBuildCanonicalMaterializationDoesNotTreatFailedPriorGenerationAsFirst(t *testing.T) {
	t.Parallel()

	sc := testScope()
	sc.PreviousGenerationExists = true
	gen := testGeneration()

	result := buildCanonicalMaterialization(sc, gen, nil)
	if result.FirstGeneration {
		t.Fatal("FirstGeneration = true, want false when a failed prior generation exists without an active generation")
	}
}

func TestBuildCanonicalMaterializationExtractsFiles(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"name":    "my-project",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/main.py",
				"relative_path": "src/main.py",
				"name":          "main.py",
				"language":      "python",
			},
		},
		{
			FactID:   "f-2",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/api/handler.go",
				"relative_path": "src/api/handler.go",
				"name":          "handler.go",
				"language":      "go",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(result.Files))
	}

	f0 := result.Files[0]
	if f0.Path != "/repos/my-project/src/main.py" {
		t.Errorf("[0].Path = %q, want %q", f0.Path, "/repos/my-project/src/main.py")
	}
	if f0.RelativePath != "src/main.py" {
		t.Errorf("[0].RelativePath = %q", f0.RelativePath)
	}
	if f0.Name != "main.py" {
		t.Errorf("[0].Name = %q", f0.Name)
	}
	if f0.Language != "python" {
		t.Errorf("[0].Language = %q", f0.Language)
	}
	if f0.DirPath != "/repos/my-project/src" {
		t.Errorf("[0].DirPath = %q, want %q", f0.DirPath, "/repos/my-project/src")
	}
	if f0.RepoID != "repo-abc" {
		t.Errorf("[0].RepoID = %q", f0.RepoID)
	}

	f1 := result.Files[1]
	if f1.Path != "/repos/my-project/src/api/handler.go" {
		t.Errorf("[1].Path = %q, want %q", f1.Path, "/repos/my-project/src/api/handler.go")
	}
	if f1.DirPath != "/repos/my-project/src/api" {
		t.Errorf("[1].DirPath = %q, want %q", f1.DirPath, "/repos/my-project/src/api")
	}
}

func TestBuildCanonicalMaterializationBuildsDirectoryChain(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"name":    "my-project",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/api/handlers/auth.go",
				"relative_path": "src/api/handlers/auth.go",
				"name":          "auth.go",
				"language":      "go",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	// Expect 3 directories: src, src/api, src/api/handlers
	if len(result.Directories) != 3 {
		t.Fatalf("len(Directories) = %d, want 3", len(result.Directories))
	}

	// Must be sorted by depth (root-first).
	for i := 1; i < len(result.Directories); i++ {
		if result.Directories[i].Depth < result.Directories[i-1].Depth {
			t.Errorf("directories not sorted by depth: [%d].Depth=%d < [%d].Depth=%d",
				i, result.Directories[i].Depth, i-1, result.Directories[i-1].Depth)
		}
	}

	d0 := result.Directories[0]
	if d0.Path != "/repos/my-project/src" {
		t.Errorf("[0].Path = %q, want %q", d0.Path, "/repos/my-project/src")
	}
	if d0.ParentPath != "/repos/my-project" {
		t.Errorf("[0].ParentPath = %q, want %q", d0.ParentPath, "/repos/my-project")
	}
	if d0.Name != "src" {
		t.Errorf("[0].Name = %q, want %q", d0.Name, "src")
	}
	if d0.Depth != 0 {
		t.Errorf("[0].Depth = %d, want 0", d0.Depth)
	}
	if d0.RepoID != "repo-abc" {
		t.Errorf("[0].RepoID = %q", d0.RepoID)
	}

	d1 := result.Directories[1]
	if d1.Path != "/repos/my-project/src/api" {
		t.Errorf("[1].Path = %q, want %q", d1.Path, "/repos/my-project/src/api")
	}
	if d1.ParentPath != "/repos/my-project/src" {
		t.Errorf("[1].ParentPath = %q, want %q", d1.ParentPath, "/repos/my-project/src")
	}
	if d1.Depth != 1 {
		t.Errorf("[1].Depth = %d, want 1", d1.Depth)
	}

	d2 := result.Directories[2]
	if d2.Path != "/repos/my-project/src/api/handlers" {
		t.Errorf("[2].Path = %q, want %q", d2.Path, "/repos/my-project/src/api/handlers")
	}
	if d2.ParentPath != "/repos/my-project/src/api" {
		t.Errorf("[2].ParentPath = %q, want %q", d2.ParentPath, "/repos/my-project/src/api")
	}
	if d2.Depth != 2 {
		t.Errorf("[2].Depth = %d, want 2", d2.Depth)
	}
}

func TestBuildCanonicalMaterializationExtractsEntities(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"name":    "my-project",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "e-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "eid-1",
				"entity_type":   "function",
				"entity_name":   "handleRequest",
				"relative_path": "src/handler.go",
				"start_line":    10,
				"end_line":      42,
				"language":      "go",
				"repo_id":       "repo-abc",
				"entity_metadata": map[string]any{
					"visibility": "public",
					"async":      "false",
				},
			},
		},
		{
			FactID:   "e-2",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "class",
				"entity_name":   "UserService",
				"relative_path": "src/service.py",
				"start_line":    1,
				"end_line":      100,
				"language":      "python",
				"repo_id":       "repo-abc",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Entities) != 2 {
		t.Fatalf("len(Entities) = %d, want 2", len(result.Entities))
	}

	e0 := result.Entities[0]
	if e0.EntityID != "eid-1" {
		t.Errorf("[0].EntityID = %q, want %q", e0.EntityID, "eid-1")
	}
	if e0.Label != "Function" {
		t.Errorf("[0].Label = %q, want %q", e0.Label, "Function")
	}
	if e0.EntityName != "handleRequest" {
		t.Errorf("[0].EntityName = %q", e0.EntityName)
	}
	if e0.RelativePath != "src/handler.go" {
		t.Errorf("[0].RelativePath = %q", e0.RelativePath)
	}
	if e0.StartLine != 10 {
		t.Errorf("[0].StartLine = %d", e0.StartLine)
	}
	if e0.EndLine != 42 {
		t.Errorf("[0].EndLine = %d", e0.EndLine)
	}
	if e0.Language != "go" {
		t.Errorf("[0].Language = %q", e0.Language)
	}
	if e0.RepoID != "repo-abc" {
		t.Errorf("[0].RepoID = %q", e0.RepoID)
	}
	if e0.Metadata == nil {
		t.Fatal("[0].Metadata is nil")
	}
	if e0.Metadata["visibility"] != "public" {
		t.Errorf("[0].Metadata[visibility] = %q", e0.Metadata["visibility"])
	}

	e1 := result.Entities[1]
	if e1.Label != "Class" {
		t.Errorf("[1].Label = %q, want %q", e1.Label, "Class")
	}
	// entity_id was empty, so should be computed via CanonicalEntityID
	if e1.EntityID == "" {
		t.Error("[1].EntityID is empty, expected computed value")
	}
}

func TestBuildCanonicalMaterializationPreservesTypeScriptClassFamilyMetadata(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "class-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-abc",
				"entity_type":   "class",
				"entity_name":   "Service",
				"relative_path": "src/service.ts",
				"start_line":    4,
				"end_line":      18,
				"language":      "typescript",
				"entity_metadata": map[string]any{
					"decorators":              []any{"@sealed"},
					"type_parameters":         []any{"T"},
					"declaration_merge_group": "Service",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"class", "namespace"},
				},
			},
		},
		{
			FactID:   "interface-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-abc",
				"entity_type":   "interface",
				"entity_name":   "Service",
				"relative_path": "src/service.ts",
				"start_line":    20,
				"end_line":      32,
				"language":      "typescript",
				"entity_metadata": map[string]any{
					"declaration_merge_group": "Service",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"class", "interface"},
				},
			},
		},
		{
			FactID:   "enum-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-abc",
				"entity_type":   "enum",
				"entity_name":   "ServiceKind",
				"relative_path": "src/service.ts",
				"start_line":    34,
				"end_line":      41,
				"language":      "typescript",
				"entity_metadata": map[string]any{
					"declaration_merge_group": "ServiceKind",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"enum", "namespace"},
				},
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)
	if len(result.Entities) != 3 {
		t.Fatalf("len(Entities) = %d, want 3", len(result.Entities))
	}

	class := result.Entities[0]
	decorators, ok := class.Metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("class.Metadata[decorators] type = %T, want []any", class.Metadata["decorators"])
	}
	if got, want := len(decorators), 1; got != want || decorators[0] != "@sealed" {
		t.Fatalf("class.Metadata[decorators] = %#v, want [@sealed]", decorators)
	}
	typeParameters, ok := class.Metadata["type_parameters"].([]any)
	if !ok {
		t.Fatalf("class.Metadata[type_parameters] type = %T, want []any", class.Metadata["type_parameters"])
	}
	if got, want := len(typeParameters), 1; got != want || typeParameters[0] != "T" {
		t.Fatalf("class.Metadata[type_parameters] = %#v, want [T]", typeParameters)
	}
	if got, want := class.Metadata["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("class.Metadata[declaration_merge_group] = %#v, want %#v", got, want)
	}

	interfaceRow := result.Entities[1]
	if got, want := interfaceRow.Metadata["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("interface.Metadata[declaration_merge_group] = %#v, want %#v", got, want)
	}
	if got, want := interfaceRow.Metadata["declaration_merge_count"], 2; got != want {
		t.Fatalf("interface.Metadata[declaration_merge_count] = %#v, want %#v", got, want)
	}

	enumRow := result.Entities[2]
	if got, want := enumRow.Metadata["declaration_merge_group"], "ServiceKind"; got != want {
		t.Fatalf("enum.Metadata[declaration_merge_group] = %#v, want %#v", got, want)
	}
	if got, want := enumRow.Metadata["declaration_merge_count"], 2; got != want {
		t.Fatalf("enum.Metadata[declaration_merge_count] = %#v, want %#v", got, want)
	}
}

func TestBuildCanonicalMaterializationExtractsModules(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "i-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"module_name":     "requests",
				"imported_module": "requests",
				"relative_path":   "src/client.py",
				"language":        "python",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Modules) != 1 {
		t.Fatalf("len(Modules) = %d, want 1", len(result.Modules))
	}
	if result.Modules[0].Name != "requests" {
		t.Errorf("Modules[0].Name = %q", result.Modules[0].Name)
	}
}

func TestBuildCanonicalMaterializationExtractsImports(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "i-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"imported_module": "requests",
				"module_name":     "requests",
				"imported_name":   "Session",
				"alias":           "req",
				"relative_path":   "src/client.py",
				"line_number":     3,
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Imports) != 1 {
		t.Fatalf("len(Imports) = %d, want 1", len(result.Imports))
	}
	imp := result.Imports[0]
	if imp.FilePath != "/repos/my-project/src/client.py" {
		t.Errorf("FilePath = %q, want %q", imp.FilePath, "/repos/my-project/src/client.py")
	}
	if imp.ModuleName != "requests" {
		t.Errorf("ModuleName = %q", imp.ModuleName)
	}
	if imp.ImportedName != "Session" {
		t.Errorf("ImportedName = %q", imp.ImportedName)
	}
	if imp.Alias != "req" {
		t.Errorf("Alias = %q", imp.Alias)
	}
	if imp.LineNumber != 3 {
		t.Errorf("LineNumber = %d", imp.LineNumber)
	}
}

func TestBuildCanonicalMaterializationExtractsParameters(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "p-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"param_name":    "ctx",
				"function_name": "handleRequest",
				"relative_path": "src/handler.go",
				"function_line": 10,
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Parameters) != 1 {
		t.Fatalf("len(Parameters) = %d, want 1", len(result.Parameters))
	}
	p := result.Parameters[0]
	if p.ParamName != "ctx" {
		t.Errorf("ParamName = %q", p.ParamName)
	}
	if p.FunctionName != "handleRequest" {
		t.Errorf("FunctionName = %q", p.FunctionName)
	}
	if p.FilePath != "/repos/my-project/src/handler.go" {
		t.Errorf("FilePath = %q, want %q", p.FilePath, "/repos/my-project/src/handler.go")
	}
	if p.FunctionLine != 10 {
		t.Errorf("FunctionLine = %d", p.FunctionLine)
	}
}

func TestBuildCanonicalMaterializationExtractsClassMembers(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "cm-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"class_name":    "UserService",
				"function_name": "get_user",
				"relative_path": "src/service.py",
				"function_line": 25,
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.ClassMembers) != 1 {
		t.Fatalf("len(ClassMembers) = %d, want 1", len(result.ClassMembers))
	}
	cm := result.ClassMembers[0]
	if cm.ClassName != "UserService" {
		t.Errorf("ClassName = %q", cm.ClassName)
	}
	if cm.FunctionName != "get_user" {
		t.Errorf("FunctionName = %q", cm.FunctionName)
	}
	if cm.FilePath != "/repos/my-project/src/service.py" {
		t.Errorf("FilePath = %q, want %q", cm.FilePath, "/repos/my-project/src/service.py")
	}
	if cm.FunctionLine != 25 {
		t.Errorf("FunctionLine = %d", cm.FunctionLine)
	}
}

func TestBuildCanonicalMaterializationExtractsNestedFunctions(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "nf-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"outer_name":    "handleRequest",
				"inner_name":    "validateInput",
				"relative_path": "src/handler.go",
				"inner_line":    15,
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.NestedFuncs) != 1 {
		t.Fatalf("len(NestedFuncs) = %d, want 1", len(result.NestedFuncs))
	}
	nf := result.NestedFuncs[0]
	if nf.OuterName != "handleRequest" {
		t.Errorf("OuterName = %q", nf.OuterName)
	}
	if nf.InnerName != "validateInput" {
		t.Errorf("InnerName = %q", nf.InnerName)
	}
	if nf.FilePath != "/repos/my-project/src/handler.go" {
		t.Errorf("FilePath = %q, want %q", nf.FilePath, "/repos/my-project/src/handler.go")
	}
	if nf.InnerLine != 15 {
		t.Errorf("InnerLine = %d", nf.InnerLine)
	}
}

func TestBuildCanonicalMaterializationHandlesEmptyFacts(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()

	// nil input
	result := buildCanonicalMaterialization(sc, gen, nil)
	if !result.IsEmpty() {
		t.Error("expected empty materialization for nil input")
	}

	// empty slice input
	result = buildCanonicalMaterialization(sc, gen, []facts.Envelope{})
	if !result.IsEmpty() {
		t.Error("expected empty materialization for empty input")
	}
}

func TestBuildCanonicalMaterializationDeduplicatesDirectories(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/main.py",
				"relative_path": "src/main.py",
				"name":          "main.py",
				"language":      "python",
			},
		},
		{
			FactID:   "f-2",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/util.py",
				"relative_path": "src/util.py",
				"name":          "util.py",
				"language":      "python",
			},
		},
		{
			FactID:   "f-3",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/api/router.py",
				"relative_path": "src/api/router.py",
				"name":          "router.py",
				"language":      "python",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	// "src" appears from all three files but should be deduped.
	// "src/api" appears from one file.
	// Total: 2 unique directories.
	if len(result.Directories) != 2 {
		var paths []string
		for _, d := range result.Directories {
			paths = append(paths, d.Path)
		}
		t.Fatalf("len(Directories) = %d, want 2; paths=%v", len(result.Directories), paths)
	}

	// Verify ordering: depth 0 first.
	if result.Directories[0].Depth != 0 {
		t.Errorf("[0].Depth = %d, want 0", result.Directories[0].Depth)
	}
	if result.Directories[1].Depth != 1 {
		t.Errorf("[1].Depth = %d, want 1", result.Directories[1].Depth)
	}
}

func TestBuildCanonicalMaterializationSkipsTombstones(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:      "f-1",
			ScopeID:     "scope-1",
			FactKind:    "file",
			IsTombstone: true,
			Payload: map[string]any{
				"path":          "/repos/my-project/src/deleted.py",
				"relative_path": "src/deleted.py",
				"name":          "deleted.py",
				"language":      "python",
			},
		},
		{
			FactID:      "e-1",
			ScopeID:     "scope-1",
			FactKind:    "content_entity",
			IsTombstone: true,
			Payload: map[string]any{
				"entity_type":   "function",
				"entity_name":   "deletedFunc",
				"relative_path": "src/deleted.py",
				"start_line":    1,
				"repo_id":       "repo-abc",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Files) != 0 {
		t.Errorf("len(Files) = %d, want 0 (tombstoned)", len(result.Files))
	}
	if len(result.Entities) != 0 {
		t.Errorf("len(Entities) = %d, want 0 (tombstoned)", len(result.Entities))
	}
}

func TestBuildCanonicalMaterializationSkipsUnmappedEntityTypes(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "e-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "unknown_type_xyz",
				"entity_name":   "SomeEntity",
				"relative_path": "src/foo.py",
				"start_line":    1,
				"repo_id":       "repo-abc",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Entities) != 0 {
		t.Errorf("len(Entities) = %d, want 0 (unmapped type)", len(result.Entities))
	}
}

func TestBuildCanonicalMaterializationExcludesModuleAndParameterFromEntities(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "e-mod",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "module",
				"entity_name":   "requests",
				"relative_path": "src/lib.py",
				"start_line":    1,
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "e-mod-pascal",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "Module",
				"entity_name":   "utils",
				"relative_path": "src/lib.py",
				"start_line":    2,
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "e-param",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "parameter",
				"entity_name":   "ctx",
				"relative_path": "src/handler.go",
				"start_line":    10,
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "e-func",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "function",
				"entity_name":   "handleRequest",
				"relative_path": "src/handler.go",
				"start_line":    10,
				"repo_id":       "repo-abc",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	// Module and Parameter entities are excluded from the entity phase
	// because they have dedicated write phases with different MERGE keys.
	// Only the Function entity should appear.
	if len(result.Entities) != 1 {
		var labels []string
		for _, e := range result.Entities {
			labels = append(labels, e.Label)
		}
		t.Fatalf("len(Entities) = %d, want 1 (Module/Parameter excluded); labels=%v",
			len(result.Entities), labels)
	}
	if result.Entities[0].Label != "Function" {
		t.Errorf("Entities[0].Label = %q, want Function", result.Entities[0].Label)
	}

	// Module entities should be extracted into Modules instead.
	// Both lowercase "module" and PascalCase "Module" should be captured.
	if len(result.Modules) != 2 {
		t.Fatalf("len(Modules) = %d, want 2", len(result.Modules))
	}
	moduleNames := map[string]bool{}
	for _, m := range result.Modules {
		moduleNames[m.Name] = true
	}
	if !moduleNames["requests"] {
		t.Error("expected Module 'requests' to be extracted")
	}
	if !moduleNames["utils"] {
		t.Error("expected Module 'utils' to be extracted")
	}
}

func TestExtractModulesFromEntitiesDeduplicatesByName(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "e-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "Module",
				"entity_name":   "Cypress",
				"relative_path": "cypress/support/commands.ts",
				"start_line":    1,
				"language":      "typescript",
			},
		},
		{
			FactID:   "e-2",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "Module",
				"entity_name":   "Cypress",
				"relative_path": "cypress/support/e2e.ts",
				"start_line":    1,
				"language":      "typescript",
			},
		},
		{
			FactID:   "e-3",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_type":   "Module",
				"entity_name":   "Fastlane",
				"relative_path": "fastlane/actions/export_certs.rb",
				"start_line":    2,
				"language":      "ruby",
			},
		},
	}

	modules := extractModulesFromEntities(envelopes)

	if len(modules) != 2 {
		t.Fatalf("len(modules) = %d, want 2 (Cypress deduped)", len(modules))
	}
	names := map[string]string{}
	for _, m := range modules {
		names[m.Name] = m.Language
	}
	if names["Cypress"] != "typescript" {
		t.Errorf("Cypress language = %q, want typescript", names["Cypress"])
	}
	if names["Fastlane"] != "ruby" {
		t.Errorf("Fastlane language = %q, want ruby", names["Fastlane"])
	}
}

func TestBuildCanonicalMaterializationUsesLegacyFactSuffix(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repositoryFact", // NormalizeFactKind strips "Fact" → "repository"
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
				"name":    "my-project",
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "fileFact", // NormalizeFactKind strips "Fact" → "file"
			Payload: map[string]any{
				"path":          "/repos/my-project/main.go",
				"relative_path": "main.go",
				"name":          "main.go",
				"language":      "go",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if result.Repository == nil {
		t.Fatal("Repository is nil for legacy suffix fact kind")
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1 for legacy suffix fact kind", len(result.Files))
	}
}

func TestEntityTypeLabelMapCoversAllSchemaLabels(t *testing.T) {
	t.Parallel()

	stmts := graph.SchemaStatements()
	labelMap := EntityTypeLabelMap()

	// Extract uid constraint labels from schema statements.
	// Format: CREATE CONSTRAINT xxx_uid_unique IF NOT EXISTS FOR (n:Label) REQUIRE n.uid IS UNIQUE
	uidRe := regexp.MustCompile(`FOR \(n:(\w+)\) REQUIRE n\.uid IS UNIQUE`)

	var schemaLabels []string
	for _, stmt := range stmts {
		matches := uidRe.FindStringSubmatch(stmt)
		if len(matches) == 2 {
			schemaLabels = append(schemaLabels, matches[1])
		}
	}

	if len(schemaLabels) == 0 {
		t.Fatal("no uid constraint labels found in schema statements")
	}

	// Build a set of labels present in entityTypeLabelMap values.
	mapLabels := make(map[string]struct{}, len(labelMap))
	for _, label := range labelMap {
		mapLabels[label] = struct{}{}
	}

	// Every uid constraint label in schema must appear in entityTypeLabelMap.
	sort.Strings(schemaLabels)
	var missing []string
	for _, label := range schemaLabels {
		if _, ok := mapLabels[label]; !ok {
			missing = append(missing, label)
		}
	}

	if len(missing) > 0 {
		t.Errorf("entityTypeLabelMap is missing labels that have uid constraints in schema: %s",
			strings.Join(missing, ", "))
	}

	// Every label in entityTypeLabelMap must have some explicit schema support
	// in graph/schema.go. Most labels are protected by constraints; a small
	// number are intentionally backed by indexes instead of uniqueness.
	allConstrainedRe := regexp.MustCompile(`FOR \(\w+:(\w+)\) REQUIRE`)
	allConstrained := make(map[string]struct{})
	for _, stmt := range stmts {
		matches := allConstrainedRe.FindStringSubmatch(stmt)
		if len(matches) == 2 {
			allConstrained[matches[1]] = struct{}{}
		}
	}

	allIndexedRe := regexp.MustCompile(`FOR \(\w+:(\w+)\) ON`)
	allIndexed := make(map[string]struct{})
	for _, stmt := range stmts {
		matches := allIndexedRe.FindStringSubmatch(stmt)
		if len(matches) == 2 {
			allIndexed[matches[1]] = struct{}{}
		}
	}

	var unconstrained []string
	for entityType, label := range labelMap {
		if _, ok := allConstrained[label]; !ok {
			if _, ok := allIndexed[label]; !ok {
				unconstrained = append(unconstrained, fmt.Sprintf("%s->%s", entityType, label))
			}
		}
	}

	if len(unconstrained) > 0 {
		sort.Strings(unconstrained)
		t.Errorf("entityTypeLabelMap has labels without any constraint in schema: %s",
			strings.Join(unconstrained, ", "))
	}
}

func TestExtractRepositoryFallsBackToLocalPath(t *testing.T) {
	t.Parallel()

	// The collector does not emit "path" or "has_remote". extractRepository
	// must fall back to local_path for Path and derive HasRemote from remote_url.
	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":    "repo-abc",
				"name":       "my-project",
				"local_path": "/home/user/repos/my-project",
				"remote_url": "https://github.com/org/my-project.git",
				"repo_slug":  "org/my-project",
				// NOTE: no "path" key and no "has_remote" key — matches real collector output
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if result.Repository == nil {
		t.Fatal("Repository is nil")
	}
	repo := result.Repository
	// Path should fall back to local_path when path is absent.
	if repo.Path != "/home/user/repos/my-project" {
		t.Errorf("Path = %q, want %q (fallback to local_path)", repo.Path, "/home/user/repos/my-project")
	}
	// HasRemote should be derived from non-empty remote_url.
	if !repo.HasRemote {
		t.Error("HasRemote = false, want true (derived from remote_url)")
	}
	// RepoPath on the materialization should be set from Repository.Path.
	if result.RepoPath != "/home/user/repos/my-project" {
		t.Errorf("RepoPath = %q, want %q", result.RepoPath, "/home/user/repos/my-project")
	}
}

func TestExtractRepositoryNoRemoteURL(t *testing.T) {
	t.Parallel()

	// Local-only repo: no remote_url, no has_remote → HasRemote should be false.
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":    "repo-local",
				"name":       "local-project",
				"local_path": "/tmp/repos/local-project",
			},
		},
	}

	repo := extractRepository(envelopes)
	if repo == nil {
		t.Fatal("Repository is nil")
	}
	if repo.HasRemote {
		t.Error("HasRemote = true, want false (no remote_url)")
	}
	if repo.Path != "/tmp/repos/local-project" {
		t.Errorf("Path = %q, want %q (fallback to local_path)", repo.Path, "/tmp/repos/local-project")
	}
}

func TestExtractEntitiesHandlesPascalCaseEntityTypes(t *testing.T) {
	t.Parallel()

	// The Go parser emits PascalCase entity_type values (e.g. "Function",
	// "Variable", "Class") while entityTypeLabelMap keys are lowercase.
	// EntityTypeLabel must normalize the lookup.
	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"relative_path": "src/handler.go",
				"language":      "go",
			},
		},
		{
			FactID:   "e-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "eid-1",
				"entity_type":   "Function", // PascalCase from parser
				"entity_name":   "handleRequest",
				"relative_path": "src/handler.go",
				"start_line":    10,
				"end_line":      42,
				"language":      "go",
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "e-2",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "eid-2",
				"entity_type":   "Variable", // PascalCase from parser
				"entity_name":   "config",
				"relative_path": "src/handler.go",
				"start_line":    5,
				"end_line":      5,
				"language":      "go",
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "e-3",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "eid-3",
				"entity_type":   "K8sResource", // PascalCase infra type
				"entity_name":   "my-deployment",
				"relative_path": "deploy/app.yaml",
				"start_line":    1,
				"end_line":      30,
				"language":      "yaml",
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "e-4",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "eid-4",
				"entity_type":   "TerraformResource", // PascalCase terraform
				"entity_name":   "aws_s3_bucket.data",
				"relative_path": "infra/main.tf",
				"start_line":    1,
				"end_line":      10,
				"language":      "hcl",
				"repo_id":       "repo-abc",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if len(result.Entities) != 4 {
		var labels []string
		for _, e := range result.Entities {
			labels = append(labels, e.Label)
		}
		t.Fatalf("len(Entities) = %d, want 4; labels=%v", len(result.Entities), labels)
	}

	// Verify all PascalCase types resolved to the correct Neo4j labels.
	expectedLabels := []string{"Function", "Variable", "K8sResource", "TerraformResource"}
	for i, want := range expectedLabels {
		if result.Entities[i].Label != want {
			t.Errorf("Entities[%d].Label = %q, want %q", i, result.Entities[i].Label, want)
		}
	}
}

func TestCanonicalMaterializationOwnsSemanticEntityLabelsExceptModule(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
	}
	canonicalOwnedSemanticLabels := []string{
		"Annotation",
		"Typedef",
		"TypeAlias",
		"TypeAnnotation",
		"Component",
		"ImplBlock",
		"Protocol",
		"ProtocolImplementation",
		"Variable",
		"Function",
	}
	for i, label := range canonicalOwnedSemanticLabels {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("e-%02d", i),
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     fmt.Sprintf("entity-%02d", i),
				"entity_type":   label,
				"entity_name":   fmt.Sprintf("Entity%d", i),
				"relative_path": "src/semantic.go",
				"start_line":    i + 1,
				"end_line":      i + 1,
				"language":      "go",
				"repo_id":       "repo-abc",
			},
		})
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)
	gotLabels := make(map[string]bool, len(result.Entities))
	for _, entity := range result.Entities {
		gotLabels[entity.Label] = true
		if entity.FilePath == "" {
			t.Fatalf("entity %q has empty FilePath; source-local must own File containment", entity.EntityID)
		}
	}
	for _, label := range canonicalOwnedSemanticLabels {
		if !gotLabels[label] {
			t.Fatalf("canonical materialization did not emit source-local entity label %q; labels=%v", label, gotLabels)
		}
	}
}

func TestEntityTypeLabelHandlesBothCases(t *testing.T) {
	t.Parallel()

	// Both lowercase and PascalCase lookups must resolve to the same label.
	cases := []struct {
		input string
		want  string
	}{
		{"function", "Function"},
		{"Function", "Function"},
		{"class", "Class"},
		{"Class", "Class"},
		{"variable", "Variable"},
		{"Variable", "Variable"},
		{"k8s_resource", "K8sResource"},
		{"K8sResource", "K8sResource"},
		{"terraform_resource", "TerraformResource"},
		{"TerraformResource", "TerraformResource"},
		{"sql_table", "SqlTable"},
		{"SqlTable", "SqlTable"},
	}

	for _, tc := range cases {
		label, ok := EntityTypeLabel(tc.input)
		if !ok {
			t.Errorf("EntityTypeLabel(%q) not found", tc.input)
			continue
		}
		if label != tc.want {
			t.Errorf("EntityTypeLabel(%q) = %q, want %q", tc.input, label, tc.want)
		}
	}
}

func TestBuildCanonicalMaterializationFallsBackToScopeMetadata(t *testing.T) {
	t.Parallel()

	// When there is no RepositoryObserved fact, repo_id and repo_path
	// should come from scope metadata.
	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"path":          "/repos/my-project/src/main.py",
				"relative_path": "src/main.py",
				"name":          "main.py",
				"language":      "python",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	if result.RepoID != "repo-abc" {
		t.Errorf("RepoID = %q, want %q (from scope metadata)", result.RepoID, "repo-abc")
	}
	if result.RepoPath != "/repos/my-project" {
		t.Errorf("RepoPath = %q, want %q (from scope metadata)", result.RepoPath, "/repos/my-project")
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}
	if result.Files[0].RepoID != "repo-abc" {
		t.Errorf("Files[0].RepoID = %q, want %q", result.Files[0].RepoID, "repo-abc")
	}
}
