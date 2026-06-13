package envregistry

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// coreScanFiles are the canonical core-platform config files whose ESHU_*
// reads must all be declared in the registry. This is the CI coverage gate for
// issue #2264: it keeps the registry from drifting away from the code it
// documents. Collector and registry-credential variables are out of scope and
// live in their own config files, which are intentionally not scanned here.
var coreScanFiles = []string{
	"internal/runtime/data_stores.go",
	"internal/runtime/config.go",
	"internal/runtime/pprof.go",
	"internal/coordinator/config.go",
}

var esuVarPattern = regexp.MustCompile(`ESHU_[A-Z0-9_]+`)

func TestRegistryCoversCoreEnvCallSites(t *testing.T) {
	t.Parallel()
	r := Default()
	goRoot := goModuleRoot(t)

	uncovered := map[string][]string{}
	for _, rel := range coreScanFiles {
		path := filepath.Join(goRoot, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		seen := map[string]struct{}{}
		for _, name := range esuVarPattern.FindAllString(string(data), -1) {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			if !r.Covers(name) {
				uncovered[name] = append(uncovered[name], rel)
			}
		}
	}

	if len(uncovered) > 0 {
		names := make([]string, 0, len(uncovered))
		for name := range uncovered {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			t.Errorf("%s is read in %v but not declared in the envregistry; add it to coreEntries", name, uncovered[name])
		}
	}
}

// goModuleRoot returns the directory containing go.mod by walking up from the
// test's working directory (the package directory).
func goModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the test working directory")
		}
		dir = parent
	}
}
