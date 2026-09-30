package main

// Contract test for #7232: the graph-anchor entity-context shapes committed to
// the B-12 snapshot must accept a realistic graph-path payload for the Dart
// mutualPing Function (id == uid, outgoing CALLS edge to mutualPong) and must
// reject the absent-entity signal (zero relationships) that the old probe
// returned on both backends. The live B-7 legs prove the graph actually
// returns the payload; this test proves the assertions can distinguish the
// two, so a future graph-path regression fails the gate instead of passing
// on the content fallback.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// TestGraphAnchorEntityContextShapes pins the assertion semantics of the
// get_entity_context?assert=graph-anchor MCP shape and its HTTP twin.
func TestGraphAnchorEntityContextShapes(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/golden/e2e-20repo-snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		QueryShapes struct {
			MCP  map[string]goldengate.QueryShape `json:"mcp"`
			HTTP map[string]goldengate.QueryShape `json:"http"`
		} `json:"query_shapes"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"id":"content-entity:e_59c56c38911d","entity_id":"content-entity:e_59c56c38911d","name":"mutualPing","labels":["Function"],"relationships":[{"type":"CALLS","target_name":"mutualPong","target_id":"content-entity:e_8cacf7638b04"}],"result_limits":{"limit":25,"ordering":"deterministic","relationship_count":1,"truncated":false,"drilldown_basis":"target_id","drilldown_tool":"get_relationship_evidence","context_path":"/api/v0/entities/content-entity:e_59c56c38911d/context"}}`)
	mcp, ok := snap.QueryShapes.MCP["get_entity_context?assert=graph-anchor"]
	if !ok {
		t.Fatal("MCP graph-anchor shape missing")
	}
	if f := goldengate.EvaluateQueryShape("get_entity_context?assert=graph-anchor", mcp, body); !f.OK {
		t.Fatalf("MCP shape finding: %+v", f)
	}
	http, ok := snap.QueryShapes.HTTP["GET /api/v0/entities/content-entity:e_59c56c38911d/context?assert=graph-anchor"]
	if !ok {
		t.Fatal("HTTP graph-anchor shape missing")
	}
	if f := goldengate.EvaluateQueryShape("GET /api/v0/entities/content-entity:e_59c56c38911d/context?assert=graph-anchor", http, body); !f.OK {
		t.Fatalf("HTTP shape finding: %+v", f)
	}
	// RED: the absent-entity signal (zero relationships) must fail the anchor shapes.
	empty := []byte(`{"id":"content-entity:e_59c56c38911d","entity_id":"content-entity:e_59c56c38911d","name":"mutualPing","labels":["Function"],"relationships":[],"result_limits":{"limit":25,"ordering":"deterministic","relationship_count":0,"truncated":false}}`)
	if f := goldengate.EvaluateQueryShape("get_entity_context?assert=graph-anchor", mcp, empty); f.OK {
		t.Fatal("MCP anchor shape passed on zero relationships; assertion cannot fail")
	} else {
		t.Logf("MCP RED as expected: %s", f.Detail)
	}
	if f := goldengate.EvaluateQueryShape("GET /api/v0/entities/content-entity:e_59c56c38911d/context?assert=graph-anchor", http, empty); f.OK {
		t.Fatal("HTTP anchor shape passed on zero relationships; assertion cannot fail")
	} else {
		t.Logf("HTTP RED as expected: %s", f.Detail)
	}
}
