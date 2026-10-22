// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// routerResponseCeiling bounds any router error response. The largest
// legitimate one carries a target's input schema and an example call.
const routerResponseCeiling = 64 << 10

// TestCallToolBoundsAHugeUnknownName is the review F1 probe: a 1 MiB name
// used to run edit distance against every registered name (10.7 s) and echo
// itself twice (4.2 MB).
func TestCallToolBoundsAHugeUnknownName(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	name := strings.Repeat("x", 1<<20)
	start := time.Now()
	raw := toolCall(t, s, context.Background(), router.CallToolName, map[string]any{"name": name, "arguments": map[string]any{}})
	elapsed := time.Since(start)
	t.Logf("response %d bytes in %s", len(raw), elapsed)
	if len(raw) > routerResponseCeiling {
		t.Fatalf("response = %d bytes, want <= %d", len(raw), routerResponseCeiling)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("call_tool with a 1 MiB name took %s, want < 2s", elapsed)
	}
	errObj, _ := decodeRouterResult(t, raw).StructuredContent["error"].(map[string]any)
	if errObj["code"] != "mcp_router_invalid_arguments" {
		t.Fatalf("code = %v, want mcp_router_invalid_arguments", errObj["code"])
	}
}

// TestCallToolBoundsAHugeInvalidArray is the review F1 probe: 200,000 ints in
// build_evidence_citation_packet.handles (maxItems 500, items objects) used
// to produce a 38 MB response of per-item field errors.
func TestCallToolBoundsAHugeInvalidArray(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	handles := make([]any, 200_000)
	for i := range handles {
		handles[i] = i
	}
	start := time.Now()
	raw := toolCall(t, s, context.Background(), router.CallToolName, map[string]any{
		"name": "build_evidence_citation_packet", "arguments": map[string]any{"handles": handles},
	})
	elapsed := time.Since(start)
	t.Logf("response %d bytes in %s", len(raw), elapsed)
	if len(raw) > routerResponseCeiling {
		t.Fatalf("response = %d bytes, want <= %d", len(raw), routerResponseCeiling)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("call_tool with 200k handles took %s, want < 2s", elapsed)
	}
	errObj, _ := decodeRouterResult(t, raw).StructuredContent["error"].(map[string]any)
	if errObj["code"] != "mcp_router_invalid_arguments" {
		t.Fatalf("code = %v, want mcp_router_invalid_arguments", errObj["code"])
	}
}

// TestCallToolBoundsManyHugeInvalidKeys covers the other echo path: object
// keys a caller controls appear in field-error paths.
func TestCallToolBoundsManyHugeInvalidKeys(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	inputs := map[string]any{}
	for i := range 2000 {
		inputs[strings.Repeat("k", 4096)+string(rune('a'+i%26))+strings.Repeat("z", i)] = i
	}
	raw := toolCall(t, s, context.Background(), router.CallToolName, map[string]any{
		"name": "resolve_query_playbook", "arguments": map[string]any{"playbook_id": "p", "inputs": inputs},
	})
	t.Logf("response %d bytes", len(raw))
	if len(raw) > routerResponseCeiling {
		t.Fatalf("response = %d bytes, want <= %d", len(raw), routerResponseCeiling)
	}
}
