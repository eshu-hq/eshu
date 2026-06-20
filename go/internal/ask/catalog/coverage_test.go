package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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
		t.Errorf("overlay is missing %d surface(s); add them to annotations_routes.go or annotations_tools.go:", len(missing))
		for _, name := range missing {
			t.Errorf("  %s", name)
		}
	}
}
