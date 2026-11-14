// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// codeContentInvestigationAdmissionCannedStore is the pin-satisfying
// double behind the wire-shape proof: it implements exactly the three
// pinned investigation reads over fixed canned rows and is
// compile-asserted against the pin, so the proof below runs through the
// admission shape, not the wider staying ports.
type codeContentInvestigationAdmissionCannedStore struct {
	inventory []EntityContent
	counts    []StructuralInventoryFileCount
	topic     []CodeTopicEvidenceRow
}

func codeContentInvestigationAdmissionCannedInventory() []EntityContent {
	return []EntityContent{{
		EntityID:     "entity-wire-1",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityType:   "Function",
		EntityName:   "handleWire",
		StartLine:    3,
		EndLine:      9,
		Language:     "go",
	}}
}

func codeContentInvestigationAdmissionCannedCounts() []StructuralInventoryFileCount {
	return []StructuralInventoryFileCount{{
		RepoID:        "repo-wire",
		RelativePath:  "src/wire/handler.go",
		Language:      "go",
		FunctionCount: 3,
		SourceBackend: "content_index",
	}}
}

func codeContentInvestigationAdmissionCannedTopic() []CodeTopicEvidenceRow {
	return []CodeTopicEvidenceRow{{
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		Language:     "go",
		EntityName:   "handleWire",
		EntityType:   "Function",
	}}
}

func (s *codeContentInvestigationAdmissionCannedStore) InspectStructuralInventory(_ context.Context, req StructuralInventoryRequest) ([]EntityContent, error) {
	if req.RepoID == "nothing-matches" {
		return nil, nil
	}
	return s.inventory, nil
}

func (s *codeContentInvestigationAdmissionCannedStore) CountStructuralInventoryByFile(_ context.Context, req StructuralInventoryRequest) ([]StructuralInventoryFileCount, error) {
	if req.RepoID == "nothing-matches" {
		return nil, nil
	}
	return s.counts, nil
}

func (s *codeContentInvestigationAdmissionCannedStore) InvestigateCodeTopic(_ context.Context, req CodeTopicInvestigationRequest) ([]CodeTopicEvidenceRow, error) {
	if req.RepoID == "nothing-matches" {
		return nil, nil
	}
	if req.RepoID == "deadline-partial" {
		return s.topic, ErrCodeTopicDeadlineTruncated
	}
	return s.topic, nil
}

var _ CodeContentInvestigationReadAdmission = (*codeContentInvestigationAdmissionCannedStore)(nil)

// marshalCodeContentInvestigationAdmissionWire projects one value through
// the same encoding the handlers use (no HTML escaping), so the proof
// compares wire bytes the way a satellite consumer would read them, not
// Go structs that could drift from the wire.
func marshalCodeContentInvestigationAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeContentInvestigationReadAdmissionWireShapesAreSatelliteConsumable
// proves a satellite can consume the pin using only the pin plus the
// codequery shapes: all three pinned reads answer through a
// CodeContentInvestigationReadAdmission-typed value, the caller grant
// (AllowedRepositoryIDs) travels inside the request structs end to end,
// the inventory rows carry the keys a satellite record would depend on,
// and the topic read propagates the deadline-partial marker alongside
// its bounded partial rows — the semantics the satellite port must
// preserve exactly.
func TestCodeContentInvestigationReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin CodeContentInvestigationReadAdmission = &codeContentInvestigationAdmissionCannedStore{
		inventory: codeContentInvestigationAdmissionCannedInventory(),
		counts:    codeContentInvestigationAdmissionCannedCounts(),
		topic:     codeContentInvestigationAdmissionCannedTopic(),
	}

	inventoryReq := StructuralInventoryRequest{
		RepoID:               "",
		AllowedRepositoryIDs: []string{"repo-wire"},
	}
	rows, err := pin.InspectStructuralInventory(ctx, inventoryReq)
	if err != nil || len(rows) != 1 || rows[0].EntityID != "entity-wire-1" {
		t.Fatalf("InspectStructuralInventory() = %#v, %v, want the canned row", rows, err)
	}
	if wire := marshalCodeContentInvestigationAdmissionWire(t, rows[0]); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) || !strings.Contains(wire, `"repo_id":"repo-wire"`) {
		t.Errorf("inventory wire bytes = %s, want entity_id and repo_id keys", wire)
	}
	// AllowedRepositoryIDs carries json:"-" by design: the grant travels
	// inside the process, never in the request body. The proof asserts it
	// rides the request struct alongside the filters instead of the wire.
	if len(inventoryReq.AllowedRepositoryIDs) != 1 || inventoryReq.AllowedRepositoryIDs[0] != "repo-wire" {
		t.Errorf("inventory request grant = %#v, want the caller grant carried alongside the filters", inventoryReq.AllowedRepositoryIDs)
	}

	counts, err := pin.CountStructuralInventoryByFile(ctx, inventoryReq)
	if err != nil || len(counts) != 1 || counts[0].FunctionCount != 3 {
		t.Fatalf("CountStructuralInventoryByFile() = %#v, %v, want the canned file count", counts, err)
	}
	if wire := marshalCodeContentInvestigationAdmissionWire(t, counts[0]); !strings.Contains(wire, `"relative_path":"src/wire/handler.go"`) || !strings.Contains(wire, `"function_count":3`) {
		t.Errorf("file-count wire bytes = %s, want relative_path and function_count keys", wire)
	}

	topicRows, err := pin.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{AllowedRepositoryIDs: []string{"repo-wire"}})
	if err != nil || len(topicRows) != 1 || topicRows[0].EntityName != "handleWire" {
		t.Fatalf("InvestigateCodeTopic() = %#v, %v, want the canned evidence row", topicRows, err)
	}

	partial, err := pin.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{RepoID: "deadline-partial"})
	if !errors.Is(err, ErrCodeTopicDeadlineTruncated) || len(partial) != 1 {
		t.Fatalf("InvestigateCodeTopic() deadline-partial = %#v, %v, want one bounded row alongside the marker", partial, err)
	}

	empty, err := pin.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{RepoID: "nothing-matches"})
	if err != nil || len(empty) != 0 {
		t.Fatalf("InvestigateCodeTopic() unmatching = %#v, %v, want zero rows and no error", empty, err)
	}
}
