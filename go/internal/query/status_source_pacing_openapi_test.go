// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestOpenAPISourcePacingMatchesTheWireType keeps the SourcePacing
// component in lockstep with the JSON the status surface encodes for
// #10882 AC6: the component's required keys equal the keys a zero
// SourcePacingJSON row serializes. Route schemas stay loose summaries
// (the collector_backpressure precedent), so the component is the
// canonical row contract, not a route reference.
func TestOpenAPISourcePacingMatchesTheWireType(t *testing.T) {
	t.Parallel()

	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	raw := query.OpenAPISpec()
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not valid JSON: %v", err)
	}
	component, ok := spec.Components.Schemas["SourcePacing"]
	if !ok {
		t.Fatal("OpenAPI has no SourcePacing component")
	}

	encoded, err := json.Marshal(collector.SourcePacingJSON{})
	if err != nil {
		t.Fatalf("marshal zero row: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
	required := append([]string(nil), component.Required...)
	sort.Strings(required)
	if got, want := strings.Join(sortedResponseKeys(wire), ","), strings.Join(required, ","); got != want {
		t.Fatalf("SourcePacingJSON encodes %s but the OpenAPI component requires %s", got, want)
	}
}
