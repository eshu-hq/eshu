package catalog

import (
	"encoding/json"
	"os"
	"testing"
)

// implementedSurfacesFromInventory reads the committed inventory and returns the
// full set of implemented api_route/mcp_tool names, INCLUDING mutating surfaces
// that Parse excludes. It lets the read-only gate reason about every surface,
// not just the ones that survive into the catalog.
func implementedSurfacesFromInventory(t *testing.T) map[string]struct{} {
	t.Helper()
	raw, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read surface inventory: %v", err)
	}
	var env inventoryEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal inventory: %v", err)
	}
	out := make(map[string]struct{})
	for _, rec := range env.Surfaces {
		if rec.Readiness != "implemented" {
			continue
		}
		kind := SurfaceKind(rec.Category)
		if kind == KindAPIRoute || kind == KindMCPTool {
			out[rec.Name] = struct{}{}
		}
	}
	return out
}

// TestParseExcludesMutatingSurfaces proves the catalog is read-only by
// construction: a side-effecting surface present in the inventory never becomes
// a callable catalog entry, while a read surface does.
func TestParseExcludesMutatingSurfaces(t *testing.T) {
	t.Parallel()
	inv := `{"version":"v1","surfaces":[
		{"category":"api_route","name":"POST /api/v0/admin/reindex","readiness":"implemented"},
		{"category":"api_route","name":"POST /api/v0/admin/work-items/query","readiness":"implemented"}
	]}`
	cat, err := Parse([]byte(inv))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := cat.Lookup("POST /api/v0/admin/reindex"); ok {
		t.Fatal("mutating route POST /api/v0/admin/reindex must be excluded from the catalog")
	}
	if _, ok := cat.Lookup("POST /api/v0/admin/work-items/query"); !ok {
		t.Fatal("read route POST /api/v0/admin/work-items/query must be present in the catalog")
	}
}

// TestKnownAdminWriteRoutesAreMutating locks the curated registry: the known
// admin recovery write routes must be classified mutating so they can never leak
// into the planner catalog.
func TestKnownAdminWriteRoutesAreMutating(t *testing.T) {
	t.Parallel()
	want := []string{
		"POST /api/v0/admin/backfill",
		"POST /api/v0/admin/dead-letter",
		"POST /api/v0/admin/refinalize",
		"POST /api/v0/admin/reindex",
		"POST /api/v0/admin/replay",
		"POST /api/v0/admin/skip",
	}
	for _, name := range want {
		if !isMutatingSurface(name) {
			t.Errorf("expected %q to be classified mutating", name)
		}
	}
}

// TestMutatingSurfacesAreImplementedAndExcluded is the registry-drift gate:
// every curated mutating name must (a) be a real implemented surface in the
// inventory (no stale entries) and (b) be absent from the parsed catalog.
func TestMutatingSurfacesAreImplementedAndExcluded(t *testing.T) {
	t.Parallel()
	implemented := implementedSurfacesFromInventory(t)
	inventoryJSON, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read surface inventory: %v", err)
	}
	cat, err := Parse(inventoryJSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for name := range mutatingSurfaces() {
		if _, ok := implemented[name]; !ok {
			t.Errorf("mutating surface %q is not an implemented inventory surface (stale entry)", name)
		}
		if _, ok := cat.Lookup(name); ok {
			t.Errorf("mutating surface %q leaked into the read-only catalog", name)
		}
	}
}

// TestEveryImplementedSurfaceIsReadOrMutating is the anti-vanish gate: every
// implemented api_route/mcp_tool must be accounted for as EITHER a read catalog
// entry OR a curated mutating surface. A surface that is neither means a new
// surface was silently dropped and must be classified.
func TestEveryImplementedSurfaceIsReadOrMutating(t *testing.T) {
	t.Parallel()
	implemented := implementedSurfacesFromInventory(t)
	inventoryJSON, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read surface inventory: %v", err)
	}
	cat, err := Parse(inventoryJSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	inCatalog := make(map[string]struct{}, len(cat.Entries()))
	for _, e := range cat.Entries() {
		inCatalog[e.Name] = struct{}{}
	}
	var unclassified []string
	for name := range implemented {
		_, isRead := inCatalog[name]
		if !isRead && !isMutatingSurface(name) {
			unclassified = append(unclassified, name)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("%d implemented surface(s) are neither a read catalog entry nor a curated mutating surface; classify them:", len(unclassified))
		for _, name := range unclassified {
			t.Errorf("  %s", name)
		}
	}
}
