package cypher

import (
	"context"
	"slices"
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
	}

	var fileRetract Statement
	var directoryRetract Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "MATCH (f:File)"):
			fileRetract = stmt
		case strings.Contains(stmt.Cypher, "MATCH (d:Directory)"):
			directoryRetract = stmt
		}
	}
	var codeRetract Statement
	for _, stmt := range writer.buildEntityRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "MATCH (n:Function)") {
			codeRetract = stmt
			break
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
		{name: "directory", stmt: directoryRetract, paramName: "directory_paths", current: "/repos/my-repo", removed: "/repos/old"},
	} {
		values, ok := tt.stmt.Parameters[tt.paramName].([]string)
		if !ok {
			t.Fatalf("%s %s parameter type = %T, want []string", tt.name, tt.paramName, tt.stmt.Parameters[tt.paramName])
		}
		if !slices.Contains(values, tt.current) {
			t.Fatalf("%s %s = %v, want current identity %q preserved", tt.name, tt.paramName, values, tt.current)
		}
		if slices.Contains(values, tt.removed) {
			t.Fatalf("%s %s = %v, removed identity %q should remain retractable", tt.name, tt.paramName, values, tt.removed)
		}
	}
	if _, ok := codeRetract.Parameters["entity_ids"]; ok {
		t.Fatalf("code entity retract carries entity_ids after current entity upsert")
	}
	if strings.Contains(codeRetract.Cypher, "IN $entity_ids") {
		t.Fatalf("code entity retract Cypher = %q, want generation-only stale cleanup", codeRetract.Cypher)
	}
}

func TestCanonicalNodeWriterRefreshesCurrentFileStructuralEdges(t *testing.T) {
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
	var entityContainmentRefreshes []Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "-[r:IMPORTS]->"):
			importRefresh = stmt
		case strings.Contains(stmt.Cypher, "]->(f:File)"):
			directoryFileRefresh = stmt
		case strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)"):
			t.Fatalf("file/entity edge refresh should be handled by entity retraction, got: %s", stmt.Cypher)
		case strings.Contains(stmt.Cypher, "{uid: row.parent_entity_id})-[r:CONTAINS]->(m)"):
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
		if !slices.Contains(values, tt.want) {
			t.Fatalf("%s %s = %v, want %q", tt.name, tt.paramName, values, tt.want)
		}
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
			if !slices.Contains(childIDs, "content-entity:function") {
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

	importRefreshPhaseIdx := -1
	entityRetractPhaseIdx := -1
	for i, phase := range writer.buildPhases(mat) {
		switch phase.name {
		case "retract":
			for _, stmt := range phase.statements {
				if strings.Contains(stmt.Cypher, "-[r:IMPORTS]->") {
					importRefreshPhaseIdx = i
				}
			}
		case "entity_retract":
			entityRetractPhaseIdx = i
		}
	}

	if importRefreshPhaseIdx < 0 {
		t.Fatal("missing import refresh statement")
	}
	if entityRetractPhaseIdx < 0 {
		t.Fatal("missing entity_retract phase")
	}
	if importRefreshPhaseIdx > entityRetractPhaseIdx {
		t.Fatalf("import refresh phase index = %d, entity_retract phase index = %d; refresh must run first",
			importRefreshPhaseIdx, entityRetractPhaseIdx)
	}
}

func TestCanonicalNodeWriterDoesNotRefreshFileEntityEdgesPerFile(t *testing.T) {
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

	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)") {
			t.Fatalf("retract still emits per-file entity refresh after entity retraction owns stale edge cleanup: %s", stmt.Cypher)
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
		if strings.Contains(stmt.Cypher, "{uid: row.parent_entity_id})-[r:CONTAINS]->(m)") {
			containmentRefreshes = append(containmentRefreshes, stmt)
		}
	}
	if got, want := len(containmentRefreshes), 2; got != want {
		t.Fatalf("entity containment refresh statement count = %d, want %d", got, want)
	}
	var rows []map[string]any
	for _, stmt := range containmentRefreshes {
		if !strings.Contains(stmt.Cypher, "MATCH (n:Class {uid: row.parent_entity_id})") &&
			!strings.Contains(stmt.Cypher, "MATCH (n:Function {uid: row.parent_entity_id})") {
			t.Fatalf("entity containment refresh Cypher = %q, want label-specific uid anchor", stmt.Cypher)
		}
		stmtRows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
		}
		rows = append(rows, stmtRows...)
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
	}{
		{name: "code", labels: canonicalNodeRetractCodeEntityLabels},
		{name: "infra", labels: canonicalNodeRetractInfraEntityLabels},
		{name: "terraform", labels: canonicalNodeRetractTerraformEntityLabels},
		{name: "cloudformation", labels: canonicalNodeRetractCloudFormationEntityLabels},
		{name: "sql", labels: canonicalNodeRetractSQLEntityLabels},
		{name: "data", labels: canonicalNodeRetractDataEntityLabels},
		{name: "oci", labels: canonicalNodeRetractOCIEntityLabels},
		{name: "package_registry", labels: canonicalNodeRetractPackageRegistryEntityLabels},
	} {
		for label := range family.labels {
			if previous, exists := covered[label]; exists {
				t.Fatalf("label %s covered by both %s and %s retract families", label, previous, family.name)
			}
			covered[label] = family.name
		}
	}
	retractLabels := make(map[string]struct{})
	for _, label := range canonicalNodeRetractEntityLabels() {
		retractLabels[label] = struct{}{}
	}

	var missing []string
	for _, label := range projector.EntityTypeLabelMap() {
		if label == "Module" || label == "Parameter" {
			continue
		}
		if _, ok := covered[label]; !ok {
			missing = append(missing, label)
			continue
		}
		if _, ok := retractLabels[label]; !ok {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("retract families missing projectable labels: %s", strings.Join(missing, ", "))
	}
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
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseFiles &&
			strings.Contains(call.Cypher, "MATCH (f:File {path: row.path})") {
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
