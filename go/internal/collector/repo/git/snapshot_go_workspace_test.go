// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestNativeRepositorySnapshotterParsesGoWorkUnderDiscoveryRules proves
// go.work reaches the gomod parser through real discovery (#8823) and keeps
// the existing exclusions: a vendor/ copy is pruned like any vendored go.mod,
// a testdata/ copy is admitted exactly as a testdata/ go.mod is, and
// go.work.sum is never parsed as a workspace.
func TestNativeRepositorySnapshotterParsesGoWorkUnderDiscoveryRules(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	workspace := "go 1.23\n\nuse (\n\t./api\n\t./worker\n)\n"
	writeCollectorTestFile(t, filepath.Join(repoRoot, "go.work"), workspace+"use \"\"\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "go.work.sum"), "golang.org/x/text v0.14.0 h1:abc=\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "vendor", "example.com", "dep", "go.work"), workspace)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "vendor", "example.com", "dep", "go.mod"), "module example.com/dep\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "testdata", "go.work"), workspace)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "testdata", "go.mod"), "module example.com/fixture\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Engine: engine, Now: func() time.Time { return now }}

	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	parsed := map[string]map[string]any{}
	for _, fileData := range got.FileData {
		path, _ := fileData["path"].(string)
		rel, relErr := filepath.Rel(resolvedRepoRoot, path)
		if relErr != nil {
			t.Fatalf("filepath.Rel(%q) error = %v", path, relErr)
		}
		parsed[filepath.ToSlash(rel)] = fileData
	}
	paths := make([]string, 0, len(parsed))
	for path := range parsed {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	want := []string{"go.work", "testdata/go.mod", "testdata/go.work"}
	if len(paths) != len(want) {
		t.Fatalf("parsed files = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("parsed files = %v, want %v", paths, want)
		}
	}

	root := parsed["go.work"]
	if got := root["lang"]; got != "gomod" {
		t.Fatalf("go.work lang = %#v, want gomod", got)
	}
	state, _ := root["gomod_state"].(map[string]any)
	if state["state"] != "parsed" || state["go_version"] != "1.23" || state["use_count"] != 3 {
		t.Fatalf("go.work gomod_state = %#v, want parsed go 1.23 with 3 use paths", state)
	}

	// Every use path, including the empty one that names the workspace root,
	// becomes a Variable content entity with a non-blank name: the content
	// writers reject a blank entity name and would fail the whole write.
	uses := map[string]int{}
	for _, entity := range got.ContentEntities {
		if entity.RelativePath != "go.work" || entity.EntityType != "Variable" {
			continue
		}
		if strings.TrimSpace(entity.EntityName) == "" {
			t.Fatalf("go.work entity at line %d has a blank name", entity.StartLine)
		}
		uses[entity.EntityName] = entity.StartLine
	}
	wantUses := map[string]int{"./api": 4, "./worker": 5, `""`: 7}
	if !reflect.DeepEqual(uses, wantUses) {
		t.Fatalf("go.work Variable entities = %v, want %v", uses, wantUses)
	}
}
