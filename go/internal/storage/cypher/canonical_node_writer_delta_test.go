// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestCanonicalNodeWriterScopesDeltaProjectionToTouchedFiles(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	err := writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID:               "scope-1",
		GenerationID:          "gen-2",
		RepoID:                "repo-1",
		RepoPath:              "/repos/repo",
		DeltaProjection:       true,
		DeltaFilePaths:        []string{"/repos/repo/changed.go", "/repos/repo/old/deleted.go"},
		DeltaDeletedFilePaths: []string{"/repos/repo/old/deleted.go"},
		DeltaDeletedDirectoryPaths: []string{
			"/repos/repo/old/emptydir",
		},
		Repository: &canonical.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "repo",
			Path:      "/repos/repo",
			LocalPath: "/repos/repo",
		},
		Files: []canonical.FileRow{
			{
				Path:         "/repos/repo/changed.go",
				RelativePath: "changed.go",
				Name:         "changed.go",
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
		Directories: []canonical.DirectoryRow{
			{Path: "/repos/repo/parent-a", Name: "parent-a", ParentPath: "/repos/repo", RepoID: "repo-1", Depth: 0},
			{Path: "/repos/repo/parent-b", Name: "parent-b", ParentPath: "/repos/repo", RepoID: "repo-1", Depth: 0},
			{Path: "/repos/repo/parent-a/child", Name: "child", ParentPath: "/repos/repo/parent-b", RepoID: "repo-1", Depth: 1},
		},
		Entities: []canonical.EntityRow{
			{
				EntityID:     "repo-1:function:changed.go:Run:1",
				Label:        "Function",
				EntityName:   "Run",
				FilePath:     "/repos/repo/changed.go",
				RelativePath: "changed.go",
				StartLine:    1,
				EndLine:      3,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var sawFileScopedRetract bool
	var sawDirectoryScopedRetract bool
	var sawExplicitDirectory bool
	var sawDirectoryParentRefresh bool
	var sawEntityScopedRetract bool
	for _, call := range exec.calls {
		phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
		if phase == "repository_cleanup" {
			t.Fatalf("delta projection emitted repository cleanup: %s", call.Cypher)
		}
		if strings.Contains(call.Cypher, "NOT f.path IN $file_paths") ||
			strings.Contains(call.Cypher, "NOT d.path IN $directory_paths") ||
			(strings.Contains(call.Cypher, "n.generation_id <> $generation_id") &&
				!strings.Contains(call.Cypher, "n.path IN $file_paths")) {
			t.Fatalf("delta projection emitted full-generation cleanup: %s", call.Cypher)
		}
		if strings.Contains(call.Cypher, "UNWIND $file_paths AS file_path") &&
			strings.Contains(call.Cypher, "MATCH (f:File {path: file_path})") {
			sawFileScopedRetract = true
			if got := call.Parameters["file_paths"]; len(got.([]string)) != 1 {
				t.Fatalf("file scoped retract paths = %#v, want only deleted paths", got)
			}
		}
		if directoryPaths, ok := call.Parameters["directory_paths"].([]string); ok {
			sawDirectoryScopedRetract = true
			for _, path := range directoryPaths {
				if path == "/repos/repo" {
					t.Fatalf("directory scoped retract included repo root: %#v", directoryPaths)
				}
				if path == "/repos/repo/old/emptydir" {
					sawExplicitDirectory = true
					if got := call.Parameters["repo_id"]; got != "repo-1" {
						t.Fatalf("explicit directory tombstone repo_id = %v, want repo-1", got)
					}
				}
			}
		}
		if strings.Contains(call.Cypher, "UNWIND $rows AS row") &&
			strings.Contains(call.Cypher, "p.path <> row.parent_path") {
			sawDirectoryParentRefresh = true
		}
		if phase == "entity_retract" &&
			strings.Contains(call.Cypher, "n.path IN $file_paths") {
			sawEntityScopedRetract = true
		}
	}

	if !sawFileScopedRetract {
		t.Fatal("missing file-path scoped delta file retract")
	}
	if !sawDirectoryScopedRetract {
		t.Fatal("missing directory-path scoped delta empty directory retract")
	}
	if !sawExplicitDirectory {
		t.Fatal("directory scoped retract did not include explicit delta tombstone")
	}
	if !sawDirectoryParentRefresh {
		t.Fatal("missing current-directory parent CONTAINS refresh for delta projection")
	}
	if !sawEntityScopedRetract {
		t.Fatal("missing file-path scoped delta entity retract")
	}
}

func TestDeltaEmptyDirectoryRetractStatementsRunLeafFirst(t *testing.T) {
	t.Parallel()

	statements := buildDeltaEmptyDirectoryRetractStatements(
		"repo-1",
		"/repos/repo",
		[]string{"/repos/repo/service/api/deleted.go"},
		nil,
	)

	if got, want := len(statements), 2; got != want {
		t.Fatalf("len(statements) = %d, want %d", got, want)
	}
	if got, want := statements[0].Parameters["directory_paths"], []string{"/repos/repo/service/api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first directory_paths = %#v, want %#v", got, want)
	}
	if got, want := statements[1].Parameters["directory_paths"], []string{"/repos/repo/service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second directory_paths = %#v, want %#v", got, want)
	}
}

func TestDeltaEmptyDirectoryRetractStatementsIncludeExplicitTombstones(t *testing.T) {
	t.Parallel()

	statements := buildDeltaEmptyDirectoryRetractStatements(
		"repo-1",
		"/repos/repo",
		[]string{"/repos/repo/service/api/deleted.go"},
		[]string{"/repos/repo/service/empty", "/outside/repo/ignored", "/repos/repo"},
	)

	if got, want := len(statements), 2; got != want {
		t.Fatalf("len(statements) = %d, want %d", got, want)
	}
	if got, want := statements[0].Parameters["directory_paths"], []string{
		"/repos/repo/service/api",
		"/repos/repo/service/empty",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first directory_paths = %#v, want %#v", got, want)
	}
	if got, want := statements[1].Parameters["directory_paths"], []string{"/repos/repo/service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second directory_paths = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterPrunesAncestorsForDirectoryOnlyDeltaTombstones(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	err := writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID:         "scope-1",
		GenerationID:    "gen-2",
		RepoID:          "repo-1",
		RepoPath:        "/repos/repo",
		DeltaProjection: true,
		DeltaDeletedDirectoryPaths: []string{
			"/repos/repo/service/empty",
		},
		Repository: &canonical.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "repo",
			Path:      "/repos/repo",
			LocalPath: "/repos/repo",
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var sawAncestorCleanup bool
	for _, call := range exec.calls {
		directoryPaths, ok := call.Parameters["directory_paths"].([]string)
		if !ok {
			continue
		}
		if strings.Contains(call.Cypher, "NOT EXISTS") &&
			reflect.DeepEqual(directoryPaths, []string{"/repos/repo/service"}) {
			sawAncestorCleanup = true
		}
	}
	if !sawAncestorCleanup {
		t.Fatalf("missing ancestor cleanup for directory-only tombstone; calls=%#v", exec.calls)
	}
}

// deltaUnionMaterialization builds the #8779 union-shaped materialization
// with the production overlay and canonical builder. Base generation A holds
// keep.go, w.go, and the nested untouched pkg/other.go (one import). Delta B
// modifies keep.go, adds x.go, and deletes w.go, so the union tree is
// {keep.go, pkg/other.go, x.go}. It returns the repo path and the
// materialization.
func deltaUnionMaterialization(t *testing.T) (string, canonical.CanonicalMaterialization) {
	t.Helper()
	repo, err := repositoryidentity.MetadataFor("proof-8779", "/repos/proof-8779", "https://github.com/eshu-proof/proof-8779.git")
	if err != nil {
		t.Fatalf("MetadataFor() error = %v", err)
	}
	repoPath := repo.LocalPath
	const scopeID = "git-repository-scope:union"
	atA := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	atB := atA.Add(time.Hour)
	fileFacts := func(gen string, at time.Time, rel, body, entity string) []facts.Envelope {
		return []facts.Envelope{
			gitcontent.FileFactEnvelope(repoPath, repo.ID, scopeID, gen, at, map[string]any{"path": repoPath + "/" + rel, "language": "go"}, false),
			gitcontent.ContentFactEnvelope(repoPath, repo.ID, scopeID, gen, at, model.ContentFileSnapshot{RelativePath: rel, Body: body, Digest: "d-" + gen + "-" + rel, Language: "go"}),
			gitcontent.ContentEntityFactEnvelope(repoPath, repo.ID, scopeID, gen, at, model.ContentEntitySnapshot{
				EntityID: "fn-" + gen + "-" + entity, RelativePath: rel, EntityType: "Function", EntityName: entity, StartLine: 2, EndLine: 2, Language: "go", IndexedAt: at,
			}),
		}
	}
	base := []facts.Envelope{gitcontent.RepositoryFactEnvelope(repoPath, repo, "run-a", scopeID, "gen-a", atA, 3, nil, false, "main", nil, false, nil, nil, false, false)}
	base = append(base, fileFacts("gen-a", atA, "keep.go", "package p\nfunc keep() {}\n", "keep")...)
	base = append(base, fileFacts("gen-a", atA, "w.go", "package p\nfunc w() {}\n", "w")...)
	base = append(base, fileFacts("gen-a", atA, "pkg/other.go", "package pkg\nimport \"fmt\"\nfunc other() {}\n", "other")...)
	// The content-entity row is last; the file row (parsed data) is third from the end.
	base[len(base)-3].Payload["parsed_file_data"] = map[string]any{"imports": []any{map[string]any{"module": "fmt", "name": "fmt", "line": 2}}}

	delta := []facts.Envelope{gitcontent.RepositoryFactEnvelope(repoPath, repo, "run-b", scopeID, "gen-b", atB, 2, nil, false, "main", nil, true, []string{"keep.go", "x.go"}, []string{"w.go"}, false, false)}
	delta = append(delta, fileFacts("gen-b", atB, "keep.go", "package p\nfunc keepNew() {}\n", "keepNew")...)
	delta = append(delta, fileFacts("gen-b", atB, "x.go", "package p\nfunc x() {}\n", "x")...)
	fileTombstone := model.FactEnvelope("file", scopeID, "gen-b", atB, "file:"+repo.ID+":w.go", map[string]any{"graph_id": repo.ID + ":w.go", "graph_kind": "file", "repo_id": repo.ID, "relative_path": "w.go", "is_dependency": false}, repoPath+"/w.go")
	fileTombstone.IsTombstone = true
	contentTombstone := model.FactEnvelope("content", scopeID, "gen-b", atB, "content:"+repo.ID+":w.go", map[string]any{"content_path": "w.go", "content_digest": "", "repo_id": repo.ID}, repoPath+"/w.go")
	contentTombstone.IsTombstone = true
	delta = append(delta, fileTombstone, contentTombstone)

	deltaScope := facts.ParseDeltaScope(delta)
	if !deltaScope.IsDelta {
		t.Fatal("delta scope not detected")
	}
	union := facts.OverlayDeltaBase(base, delta, deltaScope)
	sc := scope.IngestionScope{
		ScopeID: scopeID, SourceSystem: "git", ScopeKind: scope.KindRepository, CollectorKind: scope.CollectorGit,
		PartitionKey: repo.ID, PreviousGenerationExists: true, ActiveGenerationID: "gen-a",
		Metadata: map[string]string{"repo_id": repo.ID, "local_path": repoPath},
	}
	gen := scope.ScopeGeneration{
		GenerationID: "gen-b", ScopeID: scopeID, ObservedAt: atB, IngestedAt: atB, Status: scope.GenerationStatusPending,
		TriggerKind: scope.TriggerKindSnapshot, IsDelta: true, DeltaBaselineCommitSHA: "a", DeltaUnionBaseGenerationID: "gen-a",
	}
	mat, quarantined, err := canonical.BuildMaterialization(sc, gen, union)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("BuildMaterialization() quarantined %d facts, want 0", len(quarantined))
	}
	return repoPath, mat
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// TestCanonicalNodeWriterPinsDeltaStatementsOnUnionBase pins the canonical
// writer's statements for a delta projected over its union base (#8779). The
// production overlay, production builder, and production writer run in
// sequence against a recording executor, so the assertions hold on the
// statement parameters and order the graph backend would receive. It stands
// in for the Neo4j end-to-end proof, which this PR records as NOT_RUN.
func TestCanonicalNodeWriterPinsDeltaStatementsOnUnionBase(t *testing.T) {
	t.Parallel()

	repoPath, mat := deltaUnionMaterialization(t)
	exec := &mockExecutor{}
	if err := NewCanonicalNodeWriter(exec, 500, nil).Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	calls := exec.calls
	const delta = "gen-b"
	keepPath, otherPath, xPath, wPath := repoPath+"/keep.go", repoPath+"/pkg/other.go", repoPath+"/x.go", repoPath+"/w.go"

	phaseOf := func(call Statement) string {
		phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
		return phase
	}
	filePathsOf := func(call Statement) []string {
		paths, _ := call.Parameters["file_paths"].([]string)
		return sortedStrings(paths)
	}
	rowsOf := func(call Statement) []map[string]any {
		rows, _ := call.Parameters["rows"].([]map[string]any)
		return rows
	}
	// firstRowAfter returns the index of the first statement after `after`
	// in `phase` that has a row satisfying match, or -1.
	firstRowAfter := func(after int, phase string, match func(map[string]any) bool) int {
		for i := after + 1; i < len(calls); i++ {
			if phaseOf(calls[i]) != phase {
				continue
			}
			for _, row := range rowsOf(calls[i]) {
				if match(row) {
					return i
				}
			}
		}
		return -1
	}

	// (i) A delta never drains or runs a full-generation cleanup: every
	// generation-stamp retract is scoped by an explicit file_paths list.
	for i, call := range calls {
		if call.Drain {
			t.Fatalf("statement #%d (%s) has Drain=true; a delta must not drain", i, phaseOf(call))
		}
		if phaseOf(call) == "repository_cleanup" {
			t.Fatalf("statement #%d is a repository cleanup: %s", i, call.Cypher)
		}
		if strings.Contains(call.Cypher, "NOT f.path IN $file_paths") ||
			strings.Contains(call.Cypher, "NOT d.path IN $directory_paths") {
			t.Fatalf("statement #%d is a full-generation cleanup: %s", i, call.Cypher)
		}
		if strings.Contains(call.Cypher, "generation_id <> $generation_id") && len(filePathsOf(call)) == 0 {
			t.Fatalf("statement #%d retracts by generation without a file_paths scope: %s", i, call.Cypher)
		}
	}

	// The touched set is union-wide, not delta-wide: the retract phase
	// derives it from mat.Files (the whole union), so the untouched base file
	// pkg/other.go is retracted and re-written alongside keep.go and x.go.
	unionFiles := sortedStrings([]string{keepPath, otherPath, xPath})
	touchedFiles := sortedStrings([]string{keepPath, otherPath, xPath, wPath})

	deletedRetract := -1
	var refreshRetracts []int
	var parameterRetracts, entityRetracts []int
	for i, call := range calls {
		got := filePathsOf(call)
		_, hasGeneration := call.Parameters["generation_id"]
		switch {
		case phaseOf(call) == "retract" && len(got) > 0 && !hasGeneration && reflect.DeepEqual(got, []string{wPath}):
			deletedRetract = i
		case phaseOf(call) == "retract" && len(got) > 0 && !hasGeneration:
			refreshRetracts = append(refreshRetracts, i)
		case phaseOf(call) == "retract" && len(got) > 0 && hasGeneration:
			parameterRetracts = append(parameterRetracts, i)
		case phaseOf(call) == "entity_retract":
			entityRetracts = append(entityRetracts, i)
		}
	}

	// (ii) The deleted-file retract names only w.go and runs first.
	if deletedRetract != 0 {
		t.Fatalf("deleted-file retract position = %d (-1 = missing), want statement 0 naming only %s", deletedRetract, wPath)
	}

	// (iii) The refresh retracts (IMPORTS and Directory-CONTAINS) cover the
	// union files; the Parameter and per-label entity retracts cover the
	// union files plus the deleted path and carry the delta generation.
	if len(refreshRetracts) != 2 {
		t.Fatalf("refresh retract statements = %d, want 2 (file imports, directory contains)", len(refreshRetracts))
	}
	for _, i := range refreshRetracts {
		if got := filePathsOf(calls[i]); !reflect.DeepEqual(got, unionFiles) {
			t.Fatalf("refresh retract #%d file_paths = %v, want the union files %v", i, got, unionFiles)
		}
	}
	if len(parameterRetracts) != 1 {
		t.Fatalf("Parameter retract statements = %d, want 1", len(parameterRetracts))
	}
	if len(entityRetracts) == 0 {
		t.Fatal("no per-label entity retract statements")
	}
	for _, i := range append(append([]int(nil), parameterRetracts...), entityRetracts...) {
		if got := filePathsOf(calls[i]); !reflect.DeepEqual(got, touchedFiles) {
			t.Fatalf("retract #%d (%s) file_paths = %v, want union plus deleted %v", i, phaseOf(calls[i]), got, touchedFiles)
		}
		if got := calls[i].Parameters["generation_id"]; got != delta {
			t.Fatalf("retract #%d (%s) generation_id = %v, want %q", i, phaseOf(calls[i]), got, delta)
		}
	}

	// (iv) The files upsert re-writes every union file, the untouched base
	// file included, under the delta generation, and never the deleted file.
	fileGenerations := map[string]map[any]bool{}
	for _, call := range calls {
		if phaseOf(call) != "files" {
			continue
		}
		for _, row := range rowsOf(call) {
			path, _ := row["path"].(string)
			if fileGenerations[path] == nil {
				fileGenerations[path] = map[any]bool{}
			}
			fileGenerations[path][row["generation_id"]] = true
		}
	}
	for _, path := range []string{keepPath, otherPath, xPath} {
		if got := fileGenerations[path]; len(got) != 1 || !got[delta] {
			t.Fatalf("files upsert generations for %s = %v, want only %q", path, got, delta)
		}
	}
	if _, ok := fileGenerations[wPath]; ok {
		t.Fatalf("files upsert wrote the deleted file %s", wPath)
	}

	// (v) The untouched base file's Function is restamped with the delta
	// generation before any entity retract runs; the entity retract keys on
	// the generation stamp, so this order is what keeps it alive. The base
	// entities the overlay replaced (keep) or removed (w) are not written.
	firstEntityRetract := entityRetracts[0]
	restamped := -1
	for i, call := range calls {
		if phaseOf(call) != "entities" || call.Parameters["_eshu_entity_label"] != "Function" {
			continue
		}
		for _, row := range rowsOf(call) {
			props, _ := row["props"].(map[string]any)
			switch props["name"] {
			case "keep", "w":
				t.Fatalf("Function upsert wrote %v, a base entity the delta replaced or removed", props["name"])
			case "other":
				if props["generation_id"] != delta || props["path"] != otherPath {
					t.Fatalf("base entity props = %v, want generation_id %q on %s", props, delta, otherPath)
				}
				restamped = i
			}
		}
	}
	if restamped < 0 {
		t.Fatal("no Function upsert for the untouched base file's entity")
	}
	if restamped > firstEntityRetract {
		t.Fatalf("base entity upsert at #%d follows the first entity retract at #%d; the retract would delete it", restamped, firstEntityRetract)
	}

	// (vi) Every delete over the untouched nested base file is followed by
	// an upsert that recreates what it removed, under the delta generation.
	recreations := []struct {
		phase string
		match func(map[string]any) bool
	}{
		{"directories", func(r map[string]any) bool {
			return r["path"] == repoPath+"/pkg" && r["generation_id"] == delta
		}},
		{"directory_edges", func(r map[string]any) bool {
			return r["path"] == repoPath+"/pkg" && r["generation_id"] == delta
		}},
		{"files", func(r map[string]any) bool { return r["path"] == otherPath && r["generation_id"] == delta }},
		{"modules", func(r map[string]any) bool { return r["name"] == "fmt" }},
		{"structural_edges", func(r map[string]any) bool {
			return r["file_path"] == otherPath && r["module_import_key"] == "fmt@go" && r["generation_id"] == delta
		}},
	}
	for _, retract := range refreshRetracts {
		for _, want := range recreations {
			if firstRowAfter(retract, want.phase, want.match) < 0 {
				t.Fatalf("retract #%d over %s has no later %s upsert recreating it", retract, otherPath, want.phase)
			}
		}
	}
}
