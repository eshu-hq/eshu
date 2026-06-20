package catalog

// Annotation records the backend and cost class for one callable surface. It is
// the curated half of the catalog: the surface inventory says a surface exists;
// this overlay says how it is served.
type Annotation struct {
	Backend Backend
	Cost    CostClass
}

// annotations is the curated overlay keyed by surface name. Every implemented
// api_route and mcp_tool surface in the inventory MUST have an entry here; the
// coverage drift test (TestOverlayCoversInventory) fails otherwise. Seed entries
// below are illustrative; the drift test in Task 5 enumerates the real set to
// fill in.
func annotations() map[string]Annotation {
	return map[string]Annotation{
		// MCP tools.
		"find_symbol": {Backend: BackendNornicDB, Cost: CostLow},
		// API routes.
		"GET /api/v0/code/symbols": {Backend: BackendNornicDB, Cost: CostLow},
	}
}
