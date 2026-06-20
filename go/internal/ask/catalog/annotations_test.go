package catalog

import "testing"

func TestAnnotateAppliesOverlay(t *testing.T) {
	t.Parallel()
	// Pick a real overlay entry dynamically so the test is robust to curated
	// annotation changes. find_symbol is present in sampleInventory and must
	// appear in the overlay; read its expected values directly from annotations()
	// rather than hard-coding them.
	const probe = "find_symbol"
	overlay := annotations()
	want, ok := overlay[probe]
	if !ok {
		t.Fatalf("annotations() is missing %q; update annotations_tools.go", probe)
	}
	cat, err := Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	for _, e := range cat.Entries() {
		if e.Name == probe {
			if e.Backend != want.Backend {
				t.Fatalf("%s backend = %q, want %q (from overlay)", probe, e.Backend, want.Backend)
			}
			if e.Cost != want.Cost {
				t.Fatalf("%s cost = %q, want %q (from overlay)", probe, e.Cost, want.Cost)
			}
			return
		}
	}
	t.Fatalf("%q entry not found in parsed catalog", probe)
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
