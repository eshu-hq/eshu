package collector

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestNativeRepositorySnapshotterSingleFileTargetsBypassGitignore(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".gitignore"), "ignored.py\n")
	targetFile := filepath.Join(repoRoot, "ignored.py")
	writeCollectorTestFile(t, targetFile, "def handler():\n    return 1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "other.py"), "def other():\n    return 2\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:    repoRoot,
			FileTargets: []string{targetFile},
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got, want := got.FileCount, 1; got != want {
		t.Fatalf("FileCount = %d, want %d", got, want)
	}
	if got, want := len(got.FileData), 1; got != want {
		t.Fatalf("len(FileData) = %d, want %d", got, want)
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetFile)
	if err != nil {
		resolvedTarget = targetFile
	}
	if parsedPath := got.FileData[0]["path"]; parsedPath != resolvedTarget {
		t.Fatalf("FileData[0].path = %#v, want %q", parsedPath, resolvedTarget)
	}
	if got, want := len(got.ContentFileMetas), 1; got != want {
		t.Fatalf("len(ContentFileMetas) = %d, want %d", got, want)
	}
	if got, want := got.ContentFileMetas[0].RelativePath, "ignored.py"; got != want {
		t.Fatalf("ContentFileMetas[0].RelativePath = %q, want %q", got, want)
	}
}

func TestNativeRepositorySnapshotterCarriesExtendedParserEntityTypes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "widget.tsx"),
		`type WidgetProps = {
  label: string;
};

export function ToolbarButton({ label }: WidgetProps) {
  return <button>{label}</button>;
}
`,
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "protocols.swift"),
		`protocol Runnable {
  func run()
}
`,
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "implementations.ex"),
		`defimpl Demo.Serializable, for: Demo.Worker do
  def serialize(worker), do: worker
end
`,
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "types.c"),
		`typedef int my_int;
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

	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "TypeAlias", "WidgetProps")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "Component", "ToolbarButton")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "Protocol", "Runnable")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "ProtocolImplementation", "Demo.Serializable")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "Typedef", "my_int")
}

func TestNativeRepositorySnapshotterCarriesRustImplBlockEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "lib.rs"),
		`impl Point {
  fn new() -> Self {
    Self {}
  }
}
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

	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "ImplBlock", "Point")

	if len(got.FileData) != 1 {
		t.Fatalf("len(FileData) = %d, want 1", len(got.FileData))
	}
	implBlocks, _ := got.FileData[0]["impl_blocks"].([]map[string]any)
	if len(implBlocks) != 1 {
		t.Fatalf("len(FileData[0].impl_blocks) = %d, want 1", len(implBlocks))
	}
	if uid, _ := implBlocks[0]["uid"].(string); uid == "" {
		t.Fatal("FileData[0].impl_blocks[0].uid = empty, want canonical content entity id")
	}
}

func TestNativeRepositorySnapshotterCarriesElixirProtocolEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "protocol.ex"),
		`defprotocol Demo.Serializable do
  def serialize(data)
end
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

	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "Protocol", "Demo.Serializable")

	if len(got.FileData) != 1 {
		t.Fatalf("len(FileData) = %d, want 1", len(got.FileData))
	}
	protocols, _ := got.FileData[0]["protocols"].([]map[string]any)
	if len(protocols) != 1 {
		t.Fatalf("len(FileData[0].protocols) = %d, want 1", len(protocols))
	}
	if uid, _ := protocols[0]["uid"].(string); uid == "" {
		t.Fatal("FileData[0].protocols[0].uid = empty, want canonical content entity id")
	}
}

func TestNativeRepositorySnapshotterCarriesElixirProtocolImplementationEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "implementation.ex"),
		`defimpl Demo.Serializable, for: Demo.Worker do
  def serialize(worker), do: worker
end
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

	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "ProtocolImplementation", "Demo.Serializable")
}

func TestNativeRepositorySnapshotterCarriesTerragruntDependencyAndVariableEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "terragrunt.hcl"),
		`terraform {
  source = "../modules/app"
}

dependency "vpc" {
  config_path = "../vpc"
}

locals {
  env = "dev"
}

inputs = {
  image_tag = "latest"
}
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

	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "TerragruntConfig", "terragrunt")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "TerragruntDependency", "vpc")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "TerragruntLocal", "env")
	assertSnapshotEntityTypeAndName(t, got.ContentEntities, "TerragruntInput", "image_tag")
}
