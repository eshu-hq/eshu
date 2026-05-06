package cypher

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterRetractLeavesRemovedIdentitiesEligibleForDeletion(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/readded.go"},
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "content-entity:readded", Label: "Function"},
		},
	}

	var fileRetract Statement
	var codeRetract Statement
	var directoryRetract Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "MATCH (f:File)"):
			fileRetract = stmt
		case strings.Contains(stmt.Cypher, "n:Function OR n:Class"):
			codeRetract = stmt
		case strings.Contains(stmt.Cypher, "MATCH (d:Directory)"):
			directoryRetract = stmt
		}
	}

	for _, tt := range []struct {
		name      string
		stmt      Statement
		paramName string
		current   string
		removed   string
	}{
		{name: "file", stmt: fileRetract, paramName: "file_paths", current: "/repos/my-repo/readded.go", removed: "/repos/my-repo/deleted.go"},
		{name: "code entity", stmt: codeRetract, paramName: "entity_ids", current: "content-entity:readded", removed: "content-entity:deleted"},
		{name: "directory", stmt: directoryRetract, paramName: "directory_paths", current: "/repos/my-repo", removed: "/repos/old"},
	} {
		values, ok := tt.stmt.Parameters[tt.paramName].([]string)
		if !ok {
			t.Fatalf("%s %s parameter type = %T, want []string", tt.name, tt.paramName, tt.stmt.Parameters[tt.paramName])
		}
		if !stringSliceContains(values, tt.current) {
			t.Fatalf("%s %s = %v, want current identity %q preserved", tt.name, tt.paramName, values, tt.current)
		}
		if stringSliceContains(values, tt.removed) {
			t.Fatalf("%s %s = %v, removed identity %q should remain retractable", tt.name, tt.paramName, values, tt.removed)
		}
	}
}

func TestCanonicalNodeWriterRetractRefreshesCurrentStructuralEdges(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "content-entity:function", Label: "Function", EntityName: "ServeHTTP", FilePath: "/repos/my-repo/main.go", StartLine: 10},
			{EntityID: "content-entity:class", Label: "Class", EntityName: "Handler", FilePath: "/repos/my-repo/main.go"},
		},
		ClassMembers: []projector.ClassMemberRow{
			{ClassName: "Handler", FunctionName: "ServeHTTP", FilePath: "/repos/my-repo/main.go", FunctionLine: 10},
		},
	}

	var importRefresh Statement
	var directoryFileRefresh Statement
	var fileEntityRefresh Statement
	var entityContainmentRefreshes []Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "-[r:IMPORTS]->"):
			importRefresh = stmt
		case strings.Contains(stmt.Cypher, "]->(f:File)"):
			directoryFileRefresh = stmt
		case strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)"):
			fileEntityRefresh = stmt
		case strings.Contains(stmt.Cypher, "(n {uid: row.parent_entity_id})-[r:CONTAINS]->(m)"):
			entityContainmentRefreshes = append(entityContainmentRefreshes, stmt)
		}
	}

	for _, tt := range []struct {
		name      string
		stmt      Statement
		paramName string
		want      string
	}{
		{name: "imports", stmt: importRefresh, paramName: "file_paths", want: "/repos/my-repo/main.go"},
		{name: "directory file contains", stmt: directoryFileRefresh, paramName: "file_paths", want: "/repos/my-repo/main.go"},
	} {
		if tt.stmt.Cypher == "" {
			t.Fatalf("missing %s refresh statement", tt.name)
		}
		values, ok := tt.stmt.Parameters[tt.paramName].([]string)
		if !ok {
			t.Fatalf("%s %s parameter type = %T, want []string", tt.name, tt.paramName, tt.stmt.Parameters[tt.paramName])
		}
		if !stringSliceContains(values, tt.want) {
			t.Fatalf("%s %s = %v, want %q", tt.name, tt.paramName, values, tt.want)
		}
	}
	if got, want := fileEntityRefresh.Parameters["file_path"], "/repos/my-repo/main.go"; got != want {
		t.Fatalf("file entity contains file_path = %#v, want %#v", got, want)
	}
	entityIDs, ok := fileEntityRefresh.Parameters["entity_ids"].([]string)
	if !ok {
		t.Fatalf("file entity contains entity_ids type = %T, want []string", fileEntityRefresh.Parameters["entity_ids"])
	}
	if !stringSliceContains(entityIDs, "content-entity:function") {
		t.Fatalf("file entity contains entity_ids = %v, want current entity", entityIDs)
	}
	var foundClassRefresh bool
	for _, stmt := range entityContainmentRefreshes {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("entity contains rows type = %T, want []map[string]any", stmt.Parameters["rows"])
		}
		for _, row := range rows {
			if row["parent_entity_id"] != "content-entity:class" {
				continue
			}
			foundClassRefresh = true
			childIDs, ok := row["child_entity_ids"].([]string)
			if !ok {
				t.Fatalf("entity contains child_entity_ids type = %T, want []string", row["child_entity_ids"])
			}
			if !stringSliceContains(childIDs, "content-entity:function") {
				t.Fatalf("entity contains child_entity_ids = %v, want current child entity", childIDs)
			}
		}
	}
	if !foundClassRefresh {
		t.Fatal("missing class containment refresh statement")
	}
}

func TestCanonicalNodeWriterRefreshesStructuralEdgesBeforeEntityRetract(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "content-entity:function", Label: "Function"},
		},
	}

	fileEntityRefreshIdx := -1
	codeEntityRetractIdx := -1
	for i, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)"):
			fileEntityRefreshIdx = i
		case strings.Contains(stmt.Cypher, "n:Function OR n:Class"):
			codeEntityRetractIdx = i
		}
	}

	if fileEntityRefreshIdx < 0 {
		t.Fatal("missing file/entity refresh statement")
	}
	if codeEntityRetractIdx < 0 {
		t.Fatal("missing code entity retract statement")
	}
	if fileEntityRefreshIdx > codeEntityRetractIdx {
		t.Fatalf("file/entity refresh index = %d, code entity retract index = %d; refresh must run first",
			fileEntityRefreshIdx, codeEntityRetractIdx)
	}
}

func TestCanonicalNodeWriterRefreshesOnlyStaleFileEntityEdges(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/current.go"},
			{Path: "/repos/my-repo/empty.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "function-current", Label: "Function", FilePath: "/repos/my-repo/current.go"},
			{EntityID: "struct-current", Label: "Struct", FilePath: "/repos/my-repo/current.go"},
		},
	}

	var fileEntityRefreshes []Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)") {
			fileEntityRefreshes = append(fileEntityRefreshes, stmt)
		}
	}
	if got, want := len(fileEntityRefreshes), 2; got != want {
		t.Fatalf("file/entity refresh statement count = %d, want %d", got, want)
	}
	for _, stmt := range fileEntityRefreshes {
		if !strings.Contains(stmt.Cypher, "MATCH (f:File {path: $file_path})-[r:CONTAINS]->(n)") {
			t.Fatalf("refresh Cypher = %q, want single file_path anchor", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "f.path IN $file_paths") {
			t.Fatalf("refresh Cypher = %q, must not prune multiple files in one statement", stmt.Cypher)
		}
		filePath, ok := stmt.Parameters["file_path"].(string)
		if !ok {
			t.Fatalf("refresh file_path type = %T, want string", stmt.Parameters["file_path"])
		}
		entityIDs, ok := stmt.Parameters["entity_ids"].([]string)
		if !ok {
			t.Fatalf("refresh[%s] entity_ids type = %T, want []string", filePath, stmt.Parameters["entity_ids"])
		}
		switch filePath {
		case "/repos/my-repo/current.go":
			if got, want := strings.Join(entityIDs, ","), "function-current,struct-current"; got != want {
				t.Fatalf("refresh[%s] entity_ids = %q, want %q", filePath, got, want)
			}
		case "/repos/my-repo/empty.go":
			if len(entityIDs) != 0 {
				t.Fatalf("refresh[%s] entity_ids = %#v, want empty", filePath, entityIDs)
			}
		default:
			t.Fatalf("unexpected refresh file_path %q", filePath)
		}
	}
}

func TestCanonicalNodeWriterRefreshesOnlyStaleEntityContainmentEdges(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{EntityID: "class-current", Label: "Class", EntityName: "Handler", FilePath: "/repos/my-repo/current.go"},
			{EntityID: "method-current", Label: "Function", EntityName: "ServeHTTP", FilePath: "/repos/my-repo/current.go", StartLine: 10},
			{EntityID: "function-empty", Label: "Function", EntityName: "topLevel", FilePath: "/repos/my-repo/current.go", StartLine: 30},
		},
		ClassMembers: []projector.ClassMemberRow{
			{ClassName: "Handler", FunctionName: "ServeHTTP", FilePath: "/repos/my-repo/current.go", FunctionLine: 10},
		},
	}

	var containmentRefreshes []Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "(n {uid: row.parent_entity_id})-[r:CONTAINS]->(m)") {
			containmentRefreshes = append(containmentRefreshes, stmt)
		}
	}
	if got, want := len(containmentRefreshes), 1; got != want {
		t.Fatalf("entity containment refresh statement count = %d, want %d", got, want)
	}
	rows, ok := containmentRefreshes[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", containmentRefreshes[0].Parameters["rows"])
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("rows count = %d, want %d", got, want)
	}
	for _, row := range rows {
		parentID, ok := row["parent_entity_id"].(string)
		if !ok {
			t.Fatalf("parent_entity_id type = %T, want string", row["parent_entity_id"])
		}
		childIDs, ok := row["child_entity_ids"].([]string)
		if !ok {
			t.Fatalf("child_entity_ids type = %T, want []string", row["child_entity_ids"])
		}
		switch parentID {
		case "class-current":
			if got, want := strings.Join(childIDs, ","), "method-current"; got != want {
				t.Fatalf("refresh[%s] child_entity_ids = %q, want %q", parentID, got, want)
			}
		case "method-current":
			if len(childIDs) != 0 {
				t.Fatalf("refresh[%s] child_entity_ids = %#v, want empty", parentID, childIDs)
			}
		case "function-empty":
			if len(childIDs) != 0 {
				t.Fatalf("refresh[%s] child_entity_ids = %#v, want empty", parentID, childIDs)
			}
		default:
			t.Fatalf("unexpected parent_entity_id %q", parentID)
		}
	}
}

func TestCanonicalNodeWriterRetractCoversProjectableEntityLabels(t *testing.T) {
	t.Parallel()

	covered := make(map[string]string)
	for _, family := range []struct {
		name   string
		labels map[string]struct{}
		cypher string
	}{
		{name: "code", labels: canonicalNodeRetractCodeEntityLabels, cypher: canonicalNodeRetractCodeEntitiesCypher},
		{name: "infra", labels: canonicalNodeRetractInfraEntityLabels, cypher: canonicalNodeRetractInfraEntitiesCypher},
		{name: "terraform", labels: canonicalNodeRetractTerraformEntityLabels, cypher: canonicalNodeRetractTerraformEntitiesCypher},
		{name: "cloudformation", labels: canonicalNodeRetractCloudFormationEntityLabels, cypher: canonicalNodeRetractCloudFormationEntitiesCypher},
		{name: "sql", labels: canonicalNodeRetractSQLEntityLabels, cypher: canonicalNodeRetractSQLEntitiesCypher},
		{name: "data", labels: canonicalNodeRetractDataEntityLabels, cypher: canonicalNodeRetractDataEntitiesCypher},
	} {
		for label := range family.labels {
			if previous, exists := covered[label]; exists {
				t.Fatalf("label %s covered by both %s and %s retract families", label, previous, family.name)
			}
			covered[label] = family.name
			if !strings.Contains(family.cypher, "n:"+label) {
				t.Fatalf("%s label set includes %s but cypher does not", family.name, label)
			}
		}
	}

	var missing []string
	for _, label := range projector.EntityTypeLabelMap() {
		if label == "Module" || label == "Parameter" {
			continue
		}
		if _, ok := covered[label]; !ok {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("retract families missing projectable labels: %s", strings.Join(missing, ", "))
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestCanonicalNodeWriterEmptyMaterialization(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(exec.calls) != 0 {
		t.Fatalf("expected 0 executor calls for empty materialization, got %d", len(exec.calls))
	}
}

func TestCanonicalNodeWriterRepositoryOnly(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
			RemoteURL: "https://github.com/org/my-repo",
			RepoSlug:  "org/my-repo",
			HasRemote: true,
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Should have retraction calls + repository upsert even with no files/entities
	var repoUpsertFound bool
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert && strings.Contains(call.Cypher, "MERGE (r:Repository") {
			repoUpsertFound = true
			params := call.Parameters
			if params["repo_id"] != "repo-1" {
				t.Fatalf("repo_id = %v, want repo-1", params["repo_id"])
			}
			if params["name"] != "my-repo" {
				t.Fatalf("name = %v, want my-repo", params["name"])
			}
			if params["has_remote"] != true {
				t.Fatalf("has_remote = %v, want true", params["has_remote"])
			}
		}
	}
	if !repoUpsertFound {
		t.Fatal("expected repository upsert call")
	}
}

func TestCanonicalNodeWriterFilesCreateRepoContainsEdges(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "my-repo",
			Path:   "/repos/my-repo",
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Find the file upsert call and verify it includes REPO_CONTAINS and CONTAINS edges
	var fileCypher string
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert && strings.Contains(call.Cypher, "MERGE (f:File") {
			fileCypher = call.Cypher
			break
		}
	}
	if fileCypher == "" {
		t.Fatal("expected file upsert call")
	}
	if !strings.Contains(fileCypher, "REPO_CONTAINS") {
		t.Fatalf("file cypher missing REPO_CONTAINS: %s", fileCypher)
	}
	if !strings.Contains(fileCypher, "MATCH (d:Directory") {
		t.Fatalf("file cypher missing Directory CONTAINS edge: %s", fileCypher)
	}
}
