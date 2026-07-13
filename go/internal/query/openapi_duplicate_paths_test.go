// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOpenAPISpecHasNoDuplicatePathKeys walks the raw served spec with a
// token decoder instead of a map decode, because map decoding silently
// resolves duplicate keys last-wins -- exactly how the shadowed
// security-alert reconciliations declaration hid in the spec until #5183.
// A duplicate top-level path key means two openapi_paths_*.go fragments
// declare the same route; whichever concatenates later silently wins, and
// the loser drifts unnoticed (the shadowed copy had already lost the #5154
// scoped-support marker when it was removed).
func TestOpenAPISpecHasNoDuplicatePathKeys(t *testing.T) {
	t.Parallel()

	dec := json.NewDecoder(strings.NewReader(OpenAPISpec()))
	// Advance to the "paths" object: a "paths" token only anchors when the
	// next token opens an object, so a string VALUE that happens to equal
	// "paths" earlier in the spec cannot mis-anchor the walk.
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("scanning for paths object: %v", err)
		}
		if key, ok := tok.(string); ok && key == "paths" {
			next, err := dec.Token()
			if err != nil {
				t.Fatalf("reading token after \"paths\": %v", err)
			}
			if next == json.Delim('{') {
				break
			}
		}
	}
	seen := make(map[string]bool)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("reading path key: %v", err)
		}
		key, ok := tok.(string)
		if !ok {
			t.Fatalf("path key is not a string: %v", tok)
		}
		if seen[key] {
			t.Errorf("duplicate path key %q in served OpenAPI spec: two openapi_paths_*.go fragments declare this route; the later concatenation silently wins", key)
		}
		seen[key] = true
		// Skip the operation object for this path.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatalf("skipping value for %q: %v", key, err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no path keys found in served spec; walker is broken")
	}
}
