package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOverlayKeysAreImplementedSurfaces is the reverse-drift gate: it asserts
// that every key in the curated overlay corresponds to an implemented surface
// that actually exists in the committed inventory. A failure means a stale or
// dead overlay key was not removed when its surface was retired or renamed.
func TestOverlayKeysAreImplementedSurfaces(t *testing.T) {
	t.Parallel()
	inventoryJSON, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read surface inventory: %v", err)
	}
	cat, err := Parse(inventoryJSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Build a set of implemented surface names from the parsed catalog.
	implemented := make(map[string]struct{}, len(cat.Entries()))
	for _, e := range cat.Entries() {
		implemented[e.Name] = struct{}{}
	}
	// Every overlay key must resolve to an implemented surface.
	var stale []string
	for name := range annotations() {
		if _, ok := implemented[name]; !ok {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		t.Errorf("overlay has %d stale key(s) not present in the implemented inventory; remove them from annotations_routes.go or annotations_tools.go:", len(stale))
		for _, name := range stale {
			t.Errorf("  %s", name)
		}
	}
}

// realInventoryPath returns the absolute path to the committed
// surface-inventory.generated.json artifact. It uses the caller's file
// location so the test is position-independent inside the repo.
func realInventoryPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../go/internal/ask/catalog/coverage_test.go
	// inventory is .../go/internal/capabilitycatalog/data/surface-inventory.generated.json
	dir := filepath.Dir(file)
	return filepath.Join(dir, "..", "..", "capabilitycatalog", "data", "surface-inventory.generated.json")
}

// TestOverlayCoversInventory is the coverage drift gate: it reads the real
// committed surface inventory, parses it, applies the curated annotation
// overlay, and asserts that no implemented surface is unannotated. A failure
// means the overlay in annotations_routes.go or annotations_tools.go is
// missing one or more entries that the inventory now contains.
func TestOverlayCoversInventory(t *testing.T) {
	t.Parallel()
	inventoryJSON, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read surface inventory: %v", err)
	}
	cat, err := Parse(inventoryJSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	missing := cat.Unannotated()
	if len(missing) > 0 {
		t.Errorf("overlay is missing %d surface(s); add retrieval surfaces to annotations_routes.go or annotations_tools.go, or add non-retrieval surfaces to planner_exclusions.go:", len(missing))
		for _, name := range missing {
			t.Errorf("  %s", name)
		}
	}
}
