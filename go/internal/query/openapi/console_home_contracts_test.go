// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateConsoleHomeContracts = flag.Bool("update", false,
	"rewrite apps/console/src/api/contracts/homeContracts.json from the served spec")

// consoleHomeContractRoutes are the reads the console Home page renders. Their
// 200-response schemas are snapshotted into the console so its demo fixtures
// are validated against the real contract (homeFixtures.test.ts). Adding a
// Home read means adding it here and re-running with -update.
var consoleHomeContractRoutes = []string{
	"GET /api/v0/ecosystem/overview",
	"GET /api/v0/index-status",
	"GET /api/v0/status/collector-readiness",
	"GET /api/v0/supply-chain/impact/findings/count",
	"GET /api/v0/documentation/findings/count",
	"GET /api/v0/query-playbooks",
	"GET /api/v0/investigation-workflows",
	"GET /api/v0/catalog",
}

const consoleHomeContractsPath = "../../../../apps/console/src/api/contracts/homeContracts.json"

// TestConsoleHomeContractsSnapshotIsCurrent fails when the console's committed
// snapshot of the Home routes' 200-response schemas drifts from the served
// spec, so a contract change cannot silently leave the Home fixtures stale.
func TestConsoleHomeContractsSnapshotIsCurrent(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	paths, _ := spec["paths"].(map[string]any)

	snapshot := map[string]any{}
	for _, route := range consoleHomeContractRoutes {
		method, path, _ := strings.Cut(route, " ")
		op, ok := lookup(paths, path, strings.ToLower(method), "responses", "200", "content", "application/json", "schema")
		if !ok {
			t.Fatalf("%s: no application/json 200 schema in the served spec", route)
		}
		snapshot[route] = inlineRefs(t, op, schemas, 0)
	}

	// One compact line per route keeps the snapshot diffable per route and
	// inside the repository's per-file line cap.
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, route := range consoleHomeContractRoutes {
		key, _ := json.Marshal(route)
		body, err := json.Marshal(snapshot[route])
		if err != nil {
			t.Fatalf("marshal %s: %v", route, err)
		}
		buf.Write(key)
		buf.WriteString(": ")
		buf.Write(body)
		if i < len(consoleHomeContractRoutes)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	want := buf.Bytes()

	target := filepath.Clean(consoleHomeContractsPath)
	if *updateConsoleHomeContracts {
		if err := os.WriteFile(target, want, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
		return
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/query/openapi -run TestConsoleHomeContractsSnapshotIsCurrent -update)", target, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale against the served spec; re-run with -update and review the console fixtures", target)
	}
}

func lookup(node map[string]any, keys ...string) (map[string]any, bool) {
	current := node
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// inlineRefs replaces every components/schemas $ref with its target so the
// console snapshot is self-contained.
func inlineRefs(t *testing.T, node any, schemas map[string]any, depth int) any {
	t.Helper()
	if depth > 40 {
		t.Fatalf("schema nesting exceeds 40 levels; recursive $ref?")
	}
	switch value := node.(type) {
	case map[string]any:
		if ref, ok := value["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			target, ok := schemas[name]
			if !ok {
				t.Fatalf("unresolved $ref %s", ref)
			}
			return inlineRefs(t, target, schemas, depth+1)
		}
		out := make(map[string]any, len(value))
		for key, child := range value {
			out[key] = inlineRefs(t, child, schemas, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = inlineRefs(t, child, schemas, depth+1)
		}
		return out
	default:
		return value
	}
}
