package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathRustAnnotatesResolvedModules(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	libPath := filepath.Join(repoRoot, "src", "lib.rs")
	writeTestFile(t, filepath.Join(repoRoot, "src", "api.rs"), "pub fn handle() {}\n")
	writeTestFile(t, filepath.Join(repoRoot, "src", "platform", "unix.rs"), "pub fn open() {}\n")
	writeTestFile(t, libPath, `mod api;
#[path = "platform/unix.rs"]
mod os;
mod missing;
cfg_if::cfg_if! {
    if #[cfg(unix)] {
        mod generated;
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, libPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	api := findNamedBucketItem(t, got, "modules", "api")
	assertStringFieldValue(t, api, "resolved_path", "src/api.rs")
	assertStringFieldValue(t, api, "module_resolution_status", "resolved")

	osModule := findNamedBucketItem(t, got, "modules", "os")
	assertStringFieldValue(t, osModule, "resolved_path", "src/platform/unix.rs")
	assertStringFieldValue(t, osModule, "module_resolution_status", "resolved")

	missing := findNamedBucketItem(t, got, "modules", "missing")
	assertStringFieldValue(t, missing, "module_resolution_status", "unresolved")

	generated := findNamedBucketItem(t, got, "modules", "generated")
	assertStringFieldValue(t, generated, "module_resolution_status", "blocked")
}
