package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterRefreshesEntityContainmentWithLabelAnchors(t *testing.T) {
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

	var classRefreshes int
	var functionRefreshes int
	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "MATCH (n {uid: row.parent_entity_id})") {
			t.Fatalf("entity containment refresh uses unlabelled uid anchor: %s", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "MATCH (n:Class {uid: row.parent_entity_id})-[r:CONTAINS]->(m)") {
			classRefreshes++
			continue
		}
		if strings.Contains(stmt.Cypher, "MATCH (n:Function {uid: row.parent_entity_id})-[r:CONTAINS]->(m)") {
			functionRefreshes++
			continue
		}
	}
	if classRefreshes != 1 {
		t.Fatalf("Class containment refresh statements = %d, want 1", classRefreshes)
	}
	if functionRefreshes != 1 {
		t.Fatalf("Function containment refresh statements = %d, want 1", functionRefreshes)
	}
}

func TestCanonicalNodeWriterRetractsStaleEntitiesWithLabelAnchors(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{EntityID: "function-current", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/main.go"},
			{EntityID: "class-current", Label: "Class", EntityName: "Server", FilePath: "/repos/my-repo/main.go"},
			{EntityID: "k8s-current", Label: "K8sResource", EntityName: "deployment", FilePath: "/repos/my-repo/deploy.yaml"},
		},
	}

	var functionRetracts int
	var classRetracts int
	var k8sRetracts int
	for _, stmt := range writer.buildEntityRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "DETACH DELETE n") &&
			strings.Contains(stmt.Cypher, "MATCH (n)\nWHERE n.repo_id = $repo_id") {
			t.Fatalf("entity retract uses unlabelled node anchor: %s", stmt.Cypher)
		}
		switch {
		case strings.Contains(stmt.Cypher, "MATCH (n:Function)\nWHERE n.repo_id = $repo_id"):
			functionRetracts++
		case strings.Contains(stmt.Cypher, "MATCH (n:Class)\nWHERE n.repo_id = $repo_id"):
			classRetracts++
		case strings.Contains(stmt.Cypher, "MATCH (n:K8sResource)\nWHERE n.repo_id = $repo_id"):
			k8sRetracts++
		}
	}
	if functionRetracts != 1 {
		t.Fatalf("Function retract statements = %d, want 1", functionRetracts)
	}
	if classRetracts != 1 {
		t.Fatalf("Class retract statements = %d, want 1", classRetracts)
	}
	if k8sRetracts != 1 {
		t.Fatalf("K8sResource retract statements = %d, want 1", k8sRetracts)
	}
}

func TestCanonicalNodeWriterRetractsStaleEntitiesAfterCurrentEntityUpsert(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{EntityID: "function-current", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/main.go"},
		},
	}

	phases := writer.buildPhases(mat)
	entityPhaseIdx := -1
	entityRetractPhaseIdx := -1
	for idx, phase := range phases {
		switch phase.name {
		case "entities":
			entityPhaseIdx = idx
		case "entity_retract":
			entityRetractPhaseIdx = idx
			for _, stmt := range phase.statements {
				if strings.Contains(stmt.Cypher, "IN $entity_ids") {
					t.Fatalf("post-entity retract should not use current-id exclusion: %s", stmt.Cypher)
				}
			}
		}
	}
	if entityPhaseIdx < 0 {
		t.Fatal("missing entities phase")
	}
	if entityRetractPhaseIdx < 0 {
		t.Fatal("missing entity_retract phase")
	}
	if entityRetractPhaseIdx <= entityPhaseIdx {
		t.Fatalf("entity_retract phase index = %d, entities index = %d; stale entity cleanup must run after current entity upsert",
			entityRetractPhaseIdx, entityPhaseIdx)
	}
}

func TestCanonicalNodeWriterRetractsStaleFilesFromRepositoryAnchor(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/current.go"},
		},
	}

	var fileRetract Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "File") &&
			strings.Contains(stmt.Cypher, "DETACH DELETE f") {
			fileRetract = stmt
			break
		}
	}
	if fileRetract.Cypher == "" {
		t.Fatal("missing stale file retract statement")
	}
	if strings.HasPrefix(strings.TrimSpace(fileRetract.Cypher), "MATCH (f:File)") {
		t.Fatalf("file retract starts from broad File scan: %s", fileRetract.Cypher)
	}
	if !strings.Contains(fileRetract.Cypher, "MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)") {
		t.Fatalf("file retract Cypher = %q, want Repository-id anchored REPO_CONTAINS traversal", fileRetract.Cypher)
	}
	if !strings.Contains(fileRetract.Cypher, "f.repo_id = $repo_id") {
		t.Fatalf("file retract Cypher = %q, want repo_id guard on traversed File", fileRetract.Cypher)
	}
}

func TestCanonicalNodeRefreshStructuralEdgesSeedsFromFilePath(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		cypher string
		want   string
	}{
		{
			name:   "imports",
			cypher: canonicalNodeRefreshCurrentFileImportEdgesCypher,
			want: `MATCH (f:File)
WHERE f.path IN $file_paths
MATCH (f)-[r:IMPORTS]->(:Module)
DELETE r`,
		},
		{
			name:   "directory contains",
			cypher: canonicalNodeRefreshCurrentDirectoryFileEdgesCypher,
			want: `MATCH (f:File)
WHERE f.path IN $file_paths
MATCH (:Directory)-[r:CONTAINS]->(f)
DELETE r`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.TrimSpace(tt.cypher)
			want := strings.TrimSpace(tt.want)
			if got != want {
				t.Fatalf("Cypher = %q, want indexed file seed shape %q", got, want)
			}
		})
	}
}
