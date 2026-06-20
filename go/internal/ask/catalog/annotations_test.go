package catalog

import "testing"

func TestAnnotateAppliesOverlay(t *testing.T) {
	t.Parallel()
	cat, err := Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	for _, e := range cat.Entries() {
		if e.Name == "find_symbol" {
			if e.Backend != BackendNornicDB {
				t.Fatalf("find_symbol backend = %q, want nornicdb", e.Backend)
			}
			if e.Cost != CostLow {
				t.Fatalf("find_symbol cost = %q, want low", e.Cost)
			}
			return
		}
	}
	t.Fatal("find_symbol entry not found")
}

func TestUnannotatedReportsMissingOverlay(t *testing.T) {
	t.Parallel()
	// An implemented surface with no overlay entry must be reported.
	inv := `{"version":"v1","surfaces":[
		{"category":"mcp_tool","name":"surface_without_annotation","readiness":"implemented"}
	]}`
	cat, err := Parse([]byte(inv))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	missing := cat.Unannotated()
	if len(missing) != 1 || missing[0] != "surface_without_annotation" {
		t.Fatalf("Unannotated() = %v, want [surface_without_annotation]", missing)
	}
}

func TestAnnotationOverlayHasNoUnknownBackends(t *testing.T) {
	t.Parallel()
	for name, a := range annotations() {
		if a.Backend == BackendUnknown || a.Backend == "" {
			t.Fatalf("overlay entry %q has invalid backend %q", name, a.Backend)
		}
		switch a.Cost {
		case CostLow, CostModerate, CostHigh:
		default:
			t.Fatalf("overlay entry %q has invalid cost %q", name, a.Cost)
		}
	}
}
