package catalog

import "testing"

func TestLookup(t *testing.T) {
	t.Parallel()
	const probe = "find_symbol"
	want := annotations()[probe]
	cat, _ := Parse([]byte(sampleInventory))
	cat.Annotate()
	e, ok := cat.Lookup(probe)
	if !ok {
		t.Fatalf("expected %q present", probe)
	}
	if e.Backend != want.Backend {
		t.Fatalf("%s backend = %q, want %q (from overlay)", probe, e.Backend, want.Backend)
	}
	if e.Cost != want.Cost {
		t.Fatalf("%s cost = %q, want %q (from overlay)", probe, e.Cost, want.Cost)
	}
	if _, ok := cat.Lookup("does_not_exist"); ok {
		t.Fatal("expected absent tool to return ok=false")
	}
}

func TestCheapestFirstOrdersByCost(t *testing.T) {
	t.Parallel()
	c := &Catalog{entries: []Entry{
		{Kind: KindMCPTool, Name: "b_high", Backend: BackendPostgres, Cost: CostHigh},
		{Kind: KindMCPTool, Name: "a_low", Backend: BackendNornicDB, Cost: CostLow},
		{Kind: KindMCPTool, Name: "c_moderate", Backend: BackendBoth, Cost: CostModerate},
	}}
	order := c.CheapestFirst()
	got := []string{order[0].Name, order[1].Name, order[2].Name}
	want := []string{"a_low", "c_moderate", "b_high"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CheapestFirst order = %v, want %v", got, want)
		}
	}
}

func TestByBackendFilters(t *testing.T) {
	t.Parallel()
	c := &Catalog{entries: []Entry{
		{Kind: KindMCPTool, Name: "g1", Backend: BackendNornicDB, Cost: CostLow},
		{Kind: KindMCPTool, Name: "p1", Backend: BackendPostgres, Cost: CostLow},
		{Kind: KindMCPTool, Name: "g2", Backend: BackendNornicDB, Cost: CostHigh},
	}}
	graph := c.ByBackend(BackendNornicDB)
	if len(graph) != 2 || graph[0].Name != "g1" || graph[1].Name != "g2" {
		t.Fatalf("ByBackend(nornicdb) = %+v", graph)
	}
}
