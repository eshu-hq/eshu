// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

func TestBackendAndCostConstants(t *testing.T) {
	t.Parallel()
	if BackendNornicDB != "nornicdb" || BackendPostgres != "postgres" ||
		BackendBoth != "both" || BackendEmbedded != "embedded" ||
		BackendUnknown != "unknown" {
		t.Fatalf("backend constant drift: %q %q %q %q %q",
			BackendNornicDB, BackendPostgres, BackendBoth, BackendEmbedded, BackendUnknown)
	}
	if CostLow != "low" || CostModerate != "moderate" || CostHigh != "high" {
		t.Fatalf("cost constant drift: %q %q %q", CostLow, CostModerate, CostHigh)
	}
	if KindAPIRoute != "api_route" || KindMCPTool != "mcp_tool" {
		t.Fatalf("kind constant drift: %q %q", KindAPIRoute, KindMCPTool)
	}
}

func TestEntryIsValueType(t *testing.T) {
	t.Parallel()
	e := Entry{Kind: KindMCPTool, Name: "find_symbol", Backend: BackendNornicDB, Cost: CostLow}
	if e.Name != "find_symbol" || e.Backend != BackendNornicDB || e.Cost != CostLow {
		t.Fatalf("unexpected entry: %+v", e)
	}
}
