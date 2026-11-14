// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// codeGraphScanAdmissionCannedStore is the pin-satisfying double behind
// the wire-shape proof: it implements exactly the two pinned graph-scan
// reads over fixed canned rows and is compile-asserted against the pin,
// so the proof below runs through the admission shape, not the wider
// staying ports.
type codeGraphScanAdmissionCannedStore struct {
	entity querycontract.EntityContent
}

func codeGraphScanAdmissionCannedEntity() querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     "entity-wire-1",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityType:   "Function",
		EntityName:   "handleWire",
		StartLine:    3,
		EndLine:      9,
		Language:     "go",
		SourceCache:  "func handleWire() {}",
		Metadata:     map[string]any{"wire": "entity"},
	}
}

func (s *codeGraphScanAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

func (s *codeGraphScanAdmissionCannedStore) ListRepoEntitiesByIDs(_ context.Context, _ string, ids []string, _ int) ([]querycontract.EntityContent, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []querycontract.EntityContent{s.entity}, nil
}

var _ CodeGraphScanReadAdmission = (*codeGraphScanAdmissionCannedStore)(nil)

// marshalCodeGraphScanAdmissionWire projects one value through the same
// encoding the handlers use (no HTML escaping), so the proof compares
// wire bytes the way a satellite consumer would read them, not Go
// structs that could drift from the wire.
func marshalCodeGraphScanAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeGraphScanReadAdmissionWireShapesAreSatelliteConsumable proves a
// satellite can consume the pin using only the pin plus the
// querycontract shapes: both pinned reads answer through a
// CodeGraphScanReadAdmission-typed value, the by-ID enrichment hydrates
// the wide EntityContent row (the keys the complexity by-ID branch
// merges), and an empty id set answers with no rows and no error — the
// fail-closed emptiness the grantless page relies on. A fail-closed nil
// entity marshals to null, so the satellite path has no hidden
// non-nullable expectation.
func TestCodeGraphScanReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin CodeGraphScanReadAdmission = &codeGraphScanAdmissionCannedStore{
		entity: codeGraphScanAdmissionCannedEntity(),
	}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	if wire := marshalCodeGraphScanAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}

	page, err := pin.ListRepoEntitiesByIDs(ctx, "repo-wire", []string{"entity-wire-1"}, 10)
	if err != nil || len(page) != 1 || page[0].EntityID != "entity-wire-1" {
		t.Fatalf("ListRepoEntitiesByIDs() = %#v, %v, want the canned entity", page, err)
	}
	if wire := marshalCodeGraphScanAdmissionWire(t, page[0]); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) || !strings.Contains(wire, `"repo_id":"repo-wire"`) || !strings.Contains(wire, `"relative_path":"src/wire/handler.go"`) {
		t.Errorf("entity wire bytes = %s, want entity_id, repo_id, and relative_path keys", wire)
	}

	empty, err := pin.ListRepoEntitiesByIDs(ctx, "repo-wire", nil, 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("ListRepoEntitiesByIDs() empty = %#v, %v, want zero rows and no error", empty, err)
	}
	if wire := marshalCodeGraphScanAdmissionWire(t, (*querycontract.EntityContent)(nil)); wire != "null" {
		t.Errorf("nil entity wire bytes = %s, want null (fail-closed emptiness has no hidden shape)", wire)
	}
}
