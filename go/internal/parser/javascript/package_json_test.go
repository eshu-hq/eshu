package javascript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageFileRootKindsUseNearestPackageManifest(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	packageRoot := filepath.Join(repoRoot, "packages", "worker")
	writePackageJSONTestFile(t, filepath.Join(repoRoot, "package.json"), `{"main":"packages/worker/src/root.ts"}`)
	writePackageJSONTestFile(t, filepath.Join(packageRoot, "package.json"), `{
  "main": "lib/index.js",
  "exports": {
    "./workers/*": {
      "import": "./dist/workers/*.js"
    }
  }
}`)

	indexPath := filepath.Join(packageRoot, "src", "index.ts")
	exportPath := filepath.Join(packageRoot, "src", "workers", "sync.ts")
	rootOwnedPath := filepath.Join(packageRoot, "src", "root.ts")

	assertStringSliceContains(t, PackageFileRootKinds(repoRoot, indexPath), "javascript.node_package_entrypoint")
	assertStringSliceContains(t, PackageFileRootKinds(repoRoot, exportPath), "javascript.node_package_export")
	if got := PackageFileRootKinds(repoRoot, rootOwnedPath); len(got) != 0 {
		t.Fatalf("PackageFileRootKinds(root-owned) = %#v, want no root kinds from workspace root manifest", got)
	}
}

func TestPackagePublicSourcePathsMapExportsAndTypesToSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePackageJSONTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "types": "dist/index.d.ts",
  "exports": {
    ".": "./dist/index.js"
  }
}`)
	indexPath := filepath.Join(repoRoot, "src", "index.ts")
	writePackageJSONTestFile(t, indexPath, `export function main() {}`)

	got := PackagePublicSourcePaths(repoRoot, indexPath)
	want := cleanPath(indexPath)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("PackagePublicSourcePaths() = %#v, want [%q]", got, want)
	}
}

func TestPackagePublicSourcePathsMapDeclarationTypesToSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePackageJSONTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "types": "lib/index.d.ts"
}`)
	indexPath := filepath.Join(repoRoot, "src", "index.ts")
	writePackageJSONTestFile(t, indexPath, `export function main() {}`)

	got := PackagePublicSourcePaths(repoRoot, indexPath)
	want := cleanPath(indexPath)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("PackagePublicSourcePaths() = %#v, want [%q]", got, want)
	}
}

func assertStringSliceContains(t *testing.T, values []string, want string) {
	t.Helper()

	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}

func writePackageJSONTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
