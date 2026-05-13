package collector

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestNativeRepositorySnapshotterSnapshotsOneRepository(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app.py")
	writeCollectorTestFile(
		t,
		filePath,
		"@cached\nasync def handler():\n    return 1\n\nclass Worker:\n    pass\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}

	now := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{
		Engine: engine,
		Now: func() time.Time {
			return now
		},
	}

	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:  repoRoot,
			RemoteURL: "https://github.com/example/service",
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got.RepoPath != resolvedRepoRoot {
		t.Fatalf("RepoPath = %q, want %q", got.RepoPath, resolvedRepoRoot)
	}
	if got.RemoteURL != "https://github.com/example/service" {
		t.Fatalf("RemoteURL = %q, want %q", got.RemoteURL, "https://github.com/example/service")
	}
	if got.FileCount != 1 {
		t.Fatalf("FileCount = %d, want 1", got.FileCount)
	}
	if len(got.FileData) != 1 {
		t.Fatalf("len(FileData) = %d, want 1", len(got.FileData))
	}

	parsedFile := got.FileData[0]
	functions, _ := parsedFile["functions"].([]map[string]any)
	classes, _ := parsedFile["classes"].([]map[string]any)
	if uid, _ := functions[0]["uid"].(string); uid == "" {
		t.Fatal("functions[0].uid = empty, want canonical content entity id")
	}
	if uid, _ := classes[0]["uid"].(string); uid == "" {
		t.Fatal("classes[0].uid = empty, want canonical content entity id")
	}

	if len(got.ContentFileMetas) != 1 {
		t.Fatalf("len(ContentFileMetas) = %d, want 1", len(got.ContentFileMetas))
	}
	if len(got.ContentFiles) != 0 {
		t.Fatalf("len(ContentFiles) = %d, want 0 (two-phase: bodies not retained)", len(got.ContentFiles))
	}
	contentMeta := got.ContentFileMetas[0]
	if contentMeta.RelativePath != "app.py" {
		t.Fatalf("ContentFileMetas[0].RelativePath = %q, want %q", contentMeta.RelativePath, "app.py")
	}
	if contentMeta.Digest == "" {
		t.Fatal("ContentFileMetas[0].Digest = empty, want content hash")
	}
	if contentMeta.Language != "python" {
		t.Fatalf("ContentFileMetas[0].Language = %q, want %q", contentMeta.Language, "python")
	}

	if len(got.ContentEntities) != 2 {
		t.Fatalf("len(ContentEntities) = %d, want 2", len(got.ContentEntities))
	}
	if got.ContentEntities[0].EntityName != "handler" {
		t.Fatalf("ContentEntities[0].EntityName = %q, want %q", got.ContentEntities[0].EntityName, "handler")
	}
	if got.ContentEntities[0].EntityType != "Function" {
		t.Fatalf("ContentEntities[0].EntityType = %q, want %q", got.ContentEntities[0].EntityType, "Function")
	}
	if got, want := got.ContentEntities[0].Metadata["async"], true; got != want {
		t.Fatalf("ContentEntities[0].Metadata[async] = %#v, want %#v", got, want)
	}
	if decorators, want := collectorToStringSlice(got.ContentEntities[0].Metadata["decorators"]), []string{"@cached"}; !collectorStringSlicesEqual(decorators, want) {
		t.Fatalf("ContentEntities[0].Metadata[decorators] = %#v, want %#v", got.ContentEntities[0].Metadata["decorators"], want)
	}
	if got.ContentEntities[0].IndexedAt != now {
		t.Fatalf("ContentEntities[0].IndexedAt = %v, want %v", got.ContentEntities[0].IndexedAt, now)
	}
	if got.ContentEntities[1].EntityName != "Worker" {
		t.Fatalf("ContentEntities[1].EntityName = %q, want %q", got.ContentEntities[1].EntityName, "Worker")
	}
	if got.ContentEntities[1].EntityType != "Class" {
		t.Fatalf("ContentEntities[1].EntityType = %q, want %q", got.ContentEntities[1].EntityType, "Class")
	}
}

func TestNativeRepositorySnapshotterReturnsEmptySnapshotForRepoWithoutSupportedFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "README.md"), "# hello\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got.FileCount != 0 {
		t.Fatalf("FileCount = %d, want 0", got.FileCount)
	}
	if len(got.FileData) != 0 {
		t.Fatalf("len(FileData) = %d, want 0", len(got.FileData))
	}
	if len(got.ContentFileMetas) != 0 {
		t.Fatalf("len(ContentFileMetas) = %d, want 0", len(got.ContentFileMetas))
	}
	if len(got.ContentEntities) != 0 {
		t.Fatalf("len(ContentEntities) = %d, want 0", len(got.ContentEntities))
	}
}

func TestNativeRepositorySnapshotterIncludesImportsMap(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "app.py"),
		"from helpers import Helper\n\ndef handler():\n    return Helper()\n",
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "helpers.py"),
		"class Helper:\n    pass\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	helperPaths, ok := got.ImportsMap["Helper"]
	if !ok {
		t.Fatalf("ImportsMap missing Helper entry: %#v", got.ImportsMap)
	}
	if len(helperPaths) != 1 {
		t.Fatalf("len(ImportsMap[Helper]) = %d, want 1", len(helperPaths))
	}
	if got, want := filepath.Base(helperPaths[0]), "helpers.py"; got != want {
		t.Fatalf("ImportsMap[Helper][0] base = %q, want %q", got, want)
	}

	handlerPaths, ok := got.ImportsMap["handler"]
	if !ok {
		t.Fatalf("ImportsMap missing handler entry: %#v", got.ImportsMap)
	}
	if len(handlerPaths) != 1 {
		t.Fatalf("len(ImportsMap[handler]) = %d, want 1", len(handlerPaths))
	}
	if got, want := filepath.Base(handlerPaths[0]), "app.py"; got != want {
		t.Fatalf("ImportsMap[handler][0] base = %q, want %q", got, want)
	}
}

func TestNativeRepositorySnapshotterLogsSnapshotStageTimings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "app.py"),
		"def handler():\n    return 1\n",
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "main.tf"),
		`resource "aws_s3_bucket" "logs" {}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var logs bytes.Buffer
	snapshotter := NativeRepositorySnapshotter{
		Engine:       engine,
		ParseWorkers: 2,
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	if _, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot},
	); err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	logOutput := logs.String()
	for _, want := range []string{
		`"stage":"discovery"`,
		`"stage":"pre_scan"`,
		`"stage":"go_package_semantic_prescan"`,
		`"stage":"parse"`,
		`"stage":"materialize"`,
		`"duration_seconds":`,
		`"pre_scan_workers":2`,
		`"go_package_target_count":0`,
		`"parse_workers":2`,
		`"language_parse_summary":`,
		`"language":"hcl"`,
		`"language":"python"`,
		`"file_count":1`,
		`"total_duration_seconds":`,
		`"avg_duration_seconds":`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("snapshot stage logs missing %s in %s", want, logOutput)
		}
	}
}

func TestNativeRepositorySnapshotterPreservesDependencyOwnership(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "client.py"),
		"def fetch():\n    return 1\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:     repoRoot,
			IsDependency: true,
			DisplayName:  "requests",
			Language:     "python",
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	if got.FileCount != 1 {
		t.Fatalf("FileCount = %d, want 1", got.FileCount)
	}

	parsedFile := got.FileData[0]
	if got, want := parsedFile["is_dependency"], true; got != want {
		t.Fatalf("parsed file is_dependency = %#v, want %#v", got, want)
	}
}

func TestNativeRepositorySnapshotterCarriesFileMetadataToEntitySnapshots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "main.tf"),
		`resource "aws_s3_bucket" "logs" {}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if len(got.ContentEntities) != 1 {
		t.Fatalf("len(ContentEntities) = %d, want 1", len(got.ContentEntities))
	}
	entity := got.ContentEntities[0]
	if entity.ArtifactType != "terraform_hcl" {
		t.Fatalf("ContentEntities[0].ArtifactType = %q, want %q", entity.ArtifactType, "terraform_hcl")
	}
	if entity.TemplateDialect != "" {
		t.Fatalf("ContentEntities[0].TemplateDialect = %q, want empty string", entity.TemplateDialect)
	}
	if entity.IACRelevant == nil || !*entity.IACRelevant {
		t.Fatalf("ContentEntities[0].IACRelevant = %#v, want true", entity.IACRelevant)
	}
}

func writeCollectorTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func assertSnapshotEntityTypeAndName(
	t *testing.T,
	entities []ContentEntitySnapshot,
	entityType string,
	entityName string,
) {
	t.Helper()

	for _, entity := range entities {
		if entity.EntityType == entityType && entity.EntityName == entityName {
			return
		}
	}

	t.Fatalf(
		"ContentEntities missing %s/%s in %#v",
		entityType,
		entityName,
		entities,
	)
}

func collectorToStringSlice(value any) []string {
	items, ok := value.([]string)
	if ok {
		return items
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	converted := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		text, ok := item.(string)
		if !ok {
			return nil
		}
		converted = append(converted, text)
	}
	return converted
}

func collectorStringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
