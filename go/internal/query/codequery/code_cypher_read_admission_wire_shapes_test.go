// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// codeCypherAdmissionCannedStore is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the one pinned cypher read
// over a fixed canned row and is compile-asserted against the pin, so
// the proof below runs through the admission shape, not the wider
// staying port.
type codeCypherAdmissionCannedStore struct{}

func (codeCypherAdmissionCannedStore) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	if _, ok := params["empty"]; ok {
		return nil, nil
	}
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

var _ CodeCypherReadAdmission = codeCypherAdmissionCannedStore{}

// marshalCodeCypherAdmissionWire projects one value through the same
// encoding the handlers use (no HTML escaping), so the proof compares
// wire bytes the way a satellite consumer would read them, not Go
// structs that could drift from the wire.
func marshalCodeCypherAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeCypherReadAdmissionWireShapesAreSatelliteConsumable proves a
// satellite can consume the pin using only the pin: the one pinned read
// answers through a CodeCypherReadAdmission-typed value, the canned
// graph row projects the keys a satellite record would depend on, and an
// empty answer is zero rows with no error — never a silent empty. The
// mutation-rejection, LIMIT-injection, timeout, and shared-key-only gate
// stay handler behavior OUT; this proof covers the read shape only.
func TestCodeCypherReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin CodeCypherReadAdmission = codeCypherAdmissionCannedStore{}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"limit": 1})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	if wire := marshalCodeCypherAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}

	empty, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"empty": true})
	if err != nil || len(empty) != 0 {
		t.Fatalf("Run() empty = %#v, %v, want zero rows and no error", empty, err)
	}
}
