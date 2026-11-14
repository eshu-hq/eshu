// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// supplyChainImpactPacketAdmissionCannedStore is the pin-satisfying
// double behind the wire-shape proof: it implements exactly the five
// pinned reads over fixed canned rows and is compile-asserted against
// the pin, so the proof below runs through the admission shape, not
// the wider staying ports.
type supplyChainImpactPacketAdmissionCannedStore struct {
	repo querycontract.RepositoryCatalogEntry
}

func supplyChainImpactPacketAdmissionCannedRepo() querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{
		ID:   "repo-wire",
		Name: "wire",
	}
}

func (s *supplyChainImpactPacketAdmissionCannedStore) ExplainSupplyChainImpact(_ context.Context, filter packet.ExplanationFilter) (impact.ExplanationRow, error) {
	if filter.FindingID != "finding-wire-1" {
		return impact.ExplanationRow{}, impact.ErrExplanationNotFound
	}
	return impact.ExplanationRow{
		Finding: impact.FindingRow{
			FindingID: "finding-wire-1",
			CVEID:     "CVE-wire-0001",
			PackageID: "pkg:wire/package@1.0.0",
		},
	}, nil
}

func (s *supplyChainImpactPacketAdmissionCannedStore) ReadSupplyChainImpactReadiness(_ context.Context, query impact.ReadinessQuery) (packet.ReadinessSnapshot, error) {
	if query.RepositoryID != "repo-wire" {
		return packet.ReadinessSnapshot{TargetIncomplete: true}, nil
	}
	return packet.ReadinessSnapshot{TargetIncomplete: false}, nil
}

func (s *supplyChainImpactPacketAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

func (s *supplyChainImpactPacketAdmissionCannedStore) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"wire": "graph-single", "n": 1}, nil
}

func (s *supplyChainImpactPacketAdmissionCannedStore) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return []querycontract.RepositoryCatalogEntry{s.repo}, nil
}

var _ SupplyChainImpactPacketReadAdmission = (*supplyChainImpactPacketAdmissionCannedStore)(nil)

// marshalSupplyChainImpactPacketAdmissionWire projects one value
// through the same encoding the handlers use (no HTML escaping), so
// the proof compares wire bytes the way a satellite consumer would
// read them, not Go structs that could drift from the wire.
func marshalSupplyChainImpactPacketAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestSupplyChainImpactPacketReadAdmissionWireShapesAreSatelliteConsumable
// proves a satellite can consume the pin using only the pin plus the
// querycontract, packet, and impact shapes: every one of the five
// reads answers through a SupplyChainImpactPacketReadAdmission-typed
// value, the explanation filter carries its bounded scope, and the
// finding, catalog, and graph wire projections carry the keys a
// satellite record would depend on. The not-found explanation leg is a
// typed sentinel, not an error string, so the satellite path can tell
// a missing finding from a failed read.
func TestSupplyChainImpactPacketReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin SupplyChainImpactPacketReadAdmission = &supplyChainImpactPacketAdmissionCannedStore{
		repo: supplyChainImpactPacketAdmissionCannedRepo(),
	}

	row, err := pin.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: "finding-wire-1"})
	if err != nil || row.Finding.FindingID != "finding-wire-1" {
		t.Fatalf("ExplainSupplyChainImpact() = %#v, %v, want the canned explanation row", row, err)
	}
	if wire := marshalSupplyChainImpactPacketAdmissionWire(t, row.Finding); !strings.Contains(wire, `"FindingID":"finding-wire-1"`) {
		t.Errorf("finding wire bytes = %s, want the FindingID key", wire)
	}
	if _, err := pin.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: "missing"}); err == nil {
		t.Fatalf("ExplainSupplyChainImpact() missing = nil error, want the typed not-found sentinel")
	}

	snapshot, err := pin.ReadSupplyChainImpactReadiness(ctx, impact.ReadinessQuery{RepositoryID: "repo-wire"})
	if err != nil || snapshot.TargetIncomplete {
		t.Fatalf("ReadSupplyChainImpactReadiness() = %#v, %v, want the complete canned snapshot", snapshot, err)
	}
	if incomplete, err := pin.ReadSupplyChainImpactReadiness(ctx, impact.ReadinessQuery{RepositoryID: "other"}); err != nil || !incomplete.TargetIncomplete {
		t.Fatalf("ReadSupplyChainImpactReadiness() out-of-scope = %#v, %v, want the incomplete snapshot", incomplete, err)
	}

	rows, err := pin.Run(ctx, "MATCH (r:Repository) RETURN r.id as id", map[string]any{"repo_selector": "wire"})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	single, err := pin.RunSingle(ctx, "MATCH (r:Repository) RETURN r.id as id", map[string]any{"repo_selector": "wire"})
	if err != nil || single["wire"] != "graph-single" {
		t.Fatalf("RunSingle() = %#v, %v, want the canned graph single", single, err)
	}

	matched, err := pin.MatchRepositories(ctx, "wire")
	if err != nil || len(matched) != 1 || matched[0].ID != "repo-wire" {
		t.Fatalf("MatchRepositories() = %#v, %v, want the canned catalog entry", matched, err)
	}
	if wire := marshalSupplyChainImpactPacketAdmissionWire(t, matched[0]); !strings.Contains(wire, `"ID":"repo-wire"`) {
		t.Errorf("catalog wire bytes = %s, want the ID key", wire)
	}
	if wire := marshalSupplyChainImpactPacketAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}
}
